<script lang="ts">
	import "./layout.css";
	import "@fontsource-variable/roboto-mono";
	import logo from "$lib/assets/logo.avif";
	import { resolve } from "$app/paths";

	let { children } = $props();
</script>

<svelte:head><link rel="icon" href="" /></svelte:head>
<div class="page px-3.75 py-2.5">
	<header class="flex min-h-8.75 items-center justify-between gap-2">
		<a href={resolve("/")} class="font-caption text-xl font-bold md:text-2xl"
			>Buckshot Roulette macOS port</a
		>
		<img src={logo} alt="" width="35" height="35" />
	</header>
	<nav>
		<span>All rights belong to Mike Klubnika and Critical Reflex / 2026</span>
	</nav>
	<hr />
	<main>
		{@render children()}
	</main>
	<hr />
	<footer class="flex-col max-md:flex">
		<a href="https://git.hloth.dev/hloth/buckshot-roulette-macos">website source code</a>
		<span class="float-end md:ms-4">Not affiliated with Mike Klubnika or Critical Reflex</span>
	</footer>
</div>

<style>
	.page {
		width: 100%;
		max-width: 754px;
		border: 1px solid #5c5c5c;
		border-width: 0 1px;
		background: linear-gradient(
			0deg,
			rgba(0, 0, 0, 1) 0%,
			rgba(26, 26, 26, 1) 91%,
			rgba(0, 0, 0, 1) 100%
		);
		position: relative;
	}

	.page::before,
	.page::after {
		content: "";
		position: absolute;
		top: 0;
		width: 100px;
		height: 100%;
		background: repeating-linear-gradient(30deg, #000000, #000000 25px, #ffffff 25px, #ffffff 55px);
		z-index: -1;
	}

	.page::before {
		right: 100%;
		mask-image: linear-gradient(
			90deg,
			rgba(0, 0, 0, 0) 25%,
			rgba(1, 1, 1, 0.3) 85%,
			rgba(255, 255, 255, 0.2) 98%
		);
	}

	.page::after {
		left: 100%;
		mask-image: linear-gradient(
			270deg,
			rgba(0, 0, 0, 0) 25%,
			rgba(1, 1, 1, 0.3) 85%,
			rgba(255, 255, 255, 0.2) 98%
		);
	}

	header,
	nav,
	footer {
		background: linear-gradient(
			0deg,
			rgba(0, 0, 0, 1) 0%,
			rgba(26, 26, 26, 1) 75%,
			rgba(0, 0, 0, 1) 100%
		);
	}

	header a {
		text-decoration: none;
	}

	nav {
		margin-top: 9.75px;
		font-size: 13px;
	}
</style>
