package rpc

import (
	"cmp"
	"slices"
	"strings"

	"inferencerig/backends"
	controlv1 "inferencerig/core/rpc/gen/v1"
)

func fitProto(estimate backends.FitEstimate) *controlv1.FitEstimate {
	return &controlv1.FitEstimate{
		Level:          fitLevelProto(estimate.Level),
		Reason:         estimate.Reason,
		RequiredBytes:  estimate.RequiredBytes,
		AvailableBytes: estimate.AvailableBytes,
	}
}

// fitLevels and parameterTypes translate neutral core vocabularies into their
// proto enums. A missing entry yields the zero value, which is the UNSPECIFIED
// member of both enums — exactly what an unrecognized input should map to.
var fitLevels = map[backends.FitLevel]controlv1.FitLevel{
	backends.FitFits:     controlv1.FitLevel_FIT_LEVEL_FITS,
	backends.FitMarginal: controlv1.FitLevel_FIT_LEVEL_MARGINAL,
	backends.FitTooLarge: controlv1.FitLevel_FIT_LEVEL_TOO_LARGE,
	backends.FitUnknown:  controlv1.FitLevel_FIT_LEVEL_UNKNOWN,
}

func fitLevelProto(level backends.FitLevel) controlv1.FitLevel { return fitLevels[level] }

func machineProto(host backends.HostResources) *controlv1.MachineProfile {
	return &controlv1.MachineProfile{
		TotalMemoryBytes:       uint64(max(host.TotalRAMBytes, 0)),
		AvailableMemoryBytes:   uint64(max(host.AvailableRAMBytes, 0)),
		AcceleratorName:        host.AcceleratorName,
		UnifiedMemory:          host.UnifiedMemory,
		AcceleratorMemoryBytes: uint64(max(host.VRAMBytes, 0)),
	}
}

// fitRank orders verdicts from best to worst so "at least marginal" is a
// comparison rather than a set membership test.
var fitRanks = map[controlv1.FitLevel]int{
	controlv1.FitLevel_FIT_LEVEL_FITS:      3,
	controlv1.FitLevel_FIT_LEVEL_MARGINAL:  2,
	controlv1.FitLevel_FIT_LEVEL_TOO_LARGE: 1,
}

func fitRank(level controlv1.FitLevel) int { return fitRanks[level] }

// bestVariant is the largest variant that still fits, since quality tracks size
// within a repository. It falls back to the largest variant overall when
// nothing fits, so a caller always has something to show.
func bestVariant(variants []*controlv1.ModelVariant) *controlv1.ModelVariant {
	var best, largest *controlv1.ModelVariant
	for _, variant := range variants {
		if largest == nil || variant.GetSizeBytes() > largest.GetSizeBytes() {
			largest = variant
		}
		if fitRank(variant.GetFit().GetLevel()) < fitRank(controlv1.FitLevel_FIT_LEVEL_MARGINAL) {
			continue
		}
		if best == nil || variant.GetSizeBytes() > best.GetSizeBytes() {
			best = variant
		}
	}
	if best != nil {
		return best
	}
	return largest
}

func filterByFit(models []*controlv1.CatalogModel, minimum controlv1.FitLevel) []*controlv1.CatalogModel {
	if minimum == controlv1.FitLevel_FIT_LEVEL_UNSPECIFIED {
		return models
	}
	kept := make([]*controlv1.CatalogModel, 0, len(models))
	for _, model := range models {
		if fitRank(model.GetBestVariant().GetFit().GetLevel()) >= fitRank(minimum) {
			kept = append(kept, model)
		}
	}
	return kept
}

// catalogOrders gives, per sort key, the rank a model is ordered by. Every
// order is descending — best first — so one comparison serves them all, and an
// unrecognized key leaves the catalog in the order the source returned it.
var catalogOrders = map[string]func(*controlv1.CatalogModel) int64{
	"downloads": func(m *controlv1.CatalogModel) int64 { return m.GetDownloads() },
	"likes":     func(m *controlv1.CatalogModel) int64 { return m.GetLikes() },
	"fit":       func(m *controlv1.CatalogModel) int64 { return int64(fitRank(m.GetBestVariant().GetFit().GetLevel())) },
}

func sortCatalog(models []*controlv1.CatalogModel, order string) {
	if order == "modified" {
		// The one textual rank: an ISO timestamp sorts correctly as a string.
		slices.SortStableFunc(models, func(a, b *controlv1.CatalogModel) int {
			return strings.Compare(b.GetLastModified(), a.GetLastModified())
		})
		return
	}
	rank, ok := catalogOrders[order]
	if !ok {
		return
	}
	slices.SortStableFunc(models, func(a, b *controlv1.CatalogModel) int { return cmp.Compare(rank(b), rank(a)) })
}

var parameterTypes = map[backends.ParameterType]controlv1.ParameterType{
	backends.ParameterString: controlv1.ParameterType_PARAMETER_TYPE_STRING,
	backends.ParameterInt:    controlv1.ParameterType_PARAMETER_TYPE_INT,
	backends.ParameterBool:   controlv1.ParameterType_PARAMETER_TYPE_BOOL,
	backends.ParameterList:   controlv1.ParameterType_PARAMETER_TYPE_LIST,
}

func parameterTypeProto(kind backends.ParameterType) controlv1.ParameterType {
	return parameterTypes[kind]
}
