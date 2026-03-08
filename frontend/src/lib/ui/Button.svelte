<script lang="ts">
	let {
		href,
		children,
		class: className,
	}: {
		href?: string;
		children?: import("svelte").Snippet;
		class?: import("svelte/elements").ClassValue;
	} = $props();
</script>

<svelte:element this={href ? "a" : "button"} {href} class={className}>
	{@render children?.()}
</svelte:element>

<style>
	button,
	a {
		box-shadow: inset 0px 1px 0px 0px #000000;
		background: linear-gradient(to bottom, #2e2e2e 5%, #000000 100%);
		border-radius: 3px;
		border: 1px solid #3d3d3d;
		cursor: pointer;
		color: #ffffff;
		font-size: 13px;
		padding: 6px 24px;
		text-decoration: none;
		text-shadow: 0px 1px 0px #000000;
		width: fit-content;

		&:hover {
			background: linear-gradient(to bottom, #000000 5%, #2e2e2e 100%);
		}
	}
</style>
