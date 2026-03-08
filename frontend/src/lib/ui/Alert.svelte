<script lang="ts">
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
</script>

<div class="alert" {id}>
	<div
		role="alertdialog"
		aria-modal="true"
		aria-labelledby="alert-label-{uid}"
		aria-describedby="alert-desc-{uid}"
	>
		<h2 id="alert-label-{uid}" class="mt-2 mb-8">{title}</h2>
		<div id="alert-desc-{uid}" class="alert-desc my-4">
			{@render children?.()}
		</div>
		<div class="mt-2 flex items-center justify-center text-5xl">
			<!-- svelte-ignore a11y_invalid_attribute -->
			<a href="#" class="no-underline!">Close</a>
		</div>
	</div>
</div>

<style>
	.alert {
		display: none;
		position: fixed;
		top: 0;
		left: 0;
		width: 100%;
		height: 100vh;
		z-index: 99;
		align-items: center;
		text-align: center;
		font-family: "FakeReceipt-Regular", monospace;
		background-color: rgba(0, 0, 0, 0.5);
		backdrop-filter: contrast(0.5) brightness(0.5) saturate(1);
		overflow-y: auto;
	}
	@supports (height: 100dvh) {
		.alert {
			height: 100dvh;
		}
	}
	.alert:target {
		display: flex;
	}
	[role="alertdialog"] {
		margin: auto;
		width: 800px;
		max-width: 100%;
		padding: 32px;
		background-color: black;
		border: 1px dotted white;
	}
	h2 {
		font-size: 50px;
		line-height: 50px;
		transform: scaleY(1.85);
	}
	.alert-desc {
		font-family: "DotMatrix-Regular", monospace;
		text-transform: uppercase;
		font-size: 24px;
	}
</style>
