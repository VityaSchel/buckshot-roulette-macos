<script lang="ts">
	import Modal from "./Modal.svelte";

	let {
		title,
		children,
		id,
	}: {
		title: string;
		children?: import("svelte").Snippet;
		id: string;
	} = $props();
</script>

<Modal {title} {id}>
	{@render children?.()}
	{#snippet buttons()}
		<!-- svelte-ignore a11y_invalid_attribute -->
		<a href="#" class="no-underline!">Close</a>
	{/snippet}
</Modal>
