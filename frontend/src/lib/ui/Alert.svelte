<script>
	let {
		title,
		children,
		id,
	}: {
		title: string;
		children?: import("svelte").Snippet;
		id: string;
	} = $props();

	const uid = $props.id();

	import Modal from "./Modal.svelte";
</script>

<Modal></Modal>
