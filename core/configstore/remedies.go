package configstore

import (
	"context"
	"net"

	"gopkg.in/yaml.v3"
)

// The three legal ways out of "authentication disabled on a bind that reaches
// the network". Which one is right depends on the deployment, so the store
// offers all three and the caller picks; nothing here chooses for the operator.

// RepairBindLoopback moves listen_addr to loopback, keeping the configured
// port. Only this machine can then reach the daemon, so disabling auth is no
// longer a network exposure.
func (s *FileStore) RepairBindLoopback(ctx context.Context) (WriteResult, error) {
	return s.Repair(ctx, func(document *yaml.Node) bool {
		root := documentRoot(document)
		if root == nil {
			return false
		}
		listen := mappingValue(root, "listen_addr")
		if listen == nil || listen.Kind != yaml.ScalarNode {
			return setScalar(root, []string{"listen_addr"}, "127.0.0.1:7000", "!!str")
		}
		_, port, err := net.SplitHostPort(listen.Value)
		if err != nil || port == "" {
			return false
		}
		// Rewriting Value in place keeps the node's quoting style and any
		// comment the operator attached to this line.
		return assignScalar(listen, net.JoinHostPort("127.0.0.1", port), listen.Tag)
	})
}

// RepairRequireAuth turns authentication back on, keeping the network bind.
func (s *FileStore) RepairRequireAuth(ctx context.Context) (WriteResult, error) {
	return s.Repair(ctx, func(document *yaml.Node) bool {
		return setScalar(documentRoot(document), []string{"security", "disable_auth"}, "false", "!!bool")
	})
}

// RepairAllowExposed opts into serving unauthenticated on a bind that reaches
// the network. It is the remedy that keeps the exposure rather than removing
// it, and is only ever applied because the operator asked for it by name.
func (s *FileStore) RepairAllowExposed(ctx context.Context) (WriteResult, error) {
	return s.Repair(ctx, func(document *yaml.Node) bool {
		root := documentRoot(document)
		return setScalar(root, []string{"security", "allow_exposed_without_auth"}, "true", "!!bool")
	})
}

// setScalar sets root[path...] to value, creating intermediate mappings as
// needed. It reports whether anything changed, so applying a remedy twice is a
// no-op rather than a second backup and an identical rewrite.
func setScalar(root *yaml.Node, path []string, value, tag string) bool {
	if root == nil || len(path) == 0 {
		return false
	}
	parent := root
	for _, key := range path[:len(path)-1] {
		next := mappingValue(parent, key)
		if next == nil {
			next = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			parent.Content = append(parent.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, next)
		}
		if next.Kind != yaml.MappingNode {
			return false
		}
		parent = next
	}
	leaf := path[len(path)-1]
	if current := mappingValue(parent, leaf); current != nil {
		return assignScalar(current, value, tag)
	}
	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: leaf},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value,
			HeadComment: "set by `inferencerig doctor --fix`"})
	return true
}

// assignScalar overwrites a node's value in place, preserving its comments.
// Replacing the node instead would re-home them onto whatever followed.
func assignScalar(node *yaml.Node, value, tag string) bool {
	if node.Kind == yaml.ScalarNode && node.Value == value {
		return false
	}
	node.Kind, node.Tag, node.Value = yaml.ScalarNode, tag, value
	node.Content = nil
	return true
}
