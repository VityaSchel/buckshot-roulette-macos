<script lang="ts">
	import "./layout.css";
	import "@fontsource-variable/roboto-mono";
	import logo from "$lib/assets/logo.avif";
	import { resolve } from "$app/paths";

	let { children } = $props();
</script>

<svelte:head><link rel="icon" href="" /></svelte:head>
<main class="px-3.75 py-2.5">
	<header class="flex h-8.75 justify-between">
		<a href={resolve("/")} class="font-caption text-2xl font-bold">Buckshot Roulette macOS port</a>
		<img src={logo} alt="" width="35" height="35" />
	</header>
	<nav>
		<span>All rights belong to Mike Klubnika and Critical Reflex / 2026</span>
	</nav>
	<hr />
	{@render children()}
</main>
