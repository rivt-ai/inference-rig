<script lang="ts">
	import { AlertDialog as AlertDialogPrimitive } from "bits-ui";
	import {
		buttonVariants,
		type ButtonVariant,
		type ButtonSize,
	} from "$lib/components/ui/button/index.js";
	import { cn } from "$lib/utils.js";

	let {
		ref = $bindable(null),
		class: className,
		variant = "default",
		size = "default",
		...restProps
	}: AlertDialogPrimitive.ActionProps & {
		variant?: ButtonVariant;
		size?: ButtonSize;
	} = $props();
</script>

<!--
	Built on Cancel rather than Action on purpose. bits-ui's Action primitive is
	styling only — it carries no click handler, so confirming an action left the
	dialog open on top of the work it had just triggered. Cancel is the same
	button with a close attached, and it merges our onclick ahead of its own, so
	the confirmed action still runs. Everything outward-facing (slot, variant,
	default styling) stays that of an action button.
-->
<AlertDialogPrimitive.Cancel
	bind:ref
	data-slot="alert-dialog-action"
	class={cn(buttonVariants({ variant, size }), "cn-alert-dialog-action", className)}
	{...restProps}
/>
