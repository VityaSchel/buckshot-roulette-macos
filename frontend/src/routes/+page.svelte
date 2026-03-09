<script lang="ts">
	import { PUBLIC_API_URL } from "$env/static/public";
	import mikeKlubnikaBadge from "$lib/assets/mike-klubnika-badge.gif";
	import screenshot1 from "$lib/assets/screenshot-1.avif";
	import screenshot2 from "$lib/assets/screenshot-2.avif";
	import screenshot3 from "$lib/assets/screenshot-3.avif";
	import Alert from "$lib/ui/Alert.svelte";
	import Button from "$lib/ui/Button.svelte";
	import Modal from "$lib/ui/Modal.svelte";
</script>

<svelte:head>
	<title>Buckshot Roulette for Mac</title>
	<meta property="og:title" content="Buckshot Roulette for Mac" />
</svelte:head>
<h1 class="text-lg md:text-xl">Download Buckshot Roulette for macOS</h1>
<p class="mt-4">
	<a href="https://mikeklubnika.com/" class="pixelated float-end ms-4">
		<img src={mikeKlubnikaBadge} alt="Mike Klubnika 88x31 badge" width="88" height="31" />
	</a>
	Original game by: <a href="https://mikeklubnika.com">Mike Klubnika</a>
	<br />
	macOS port published by: <a href="https://hloth.dev/">Viktor Shchelochkov (@hloth)</a>
</p>
<div class="flex flex-col items-center gap-4 xs:flex-row xs:items-start">
	<p class="mt-4">
		<b><a href="https://mikeklubnika.com/games/buckshot_roulette">BUCKSHOT ROULETTE</a></b> is a
		horror game that redesigns the infamous game of Russian Roulette, replacing the traditional
		revolver with a 12-gauge shotgun. The game was published in Steam on April 4th, 2024 for Windows
		and SteamOS + Linux but never officially ported to macOS.
		<br />
		<br />
		Buckshot Roulette for macOS project attempts to fill that gap using the game's Godot engine to compile
		native game builds for Mac.
		<br />
		<br />
		Unlike the virtualized environments that emulate Windows, the native build has no visual glitches
		or audio issues and works using native Mac's graphics APIs. Porting is made possible thanks to
		<a href="https://godotengine.org/">Godot</a>
		multi-platform compilation, game's decompilation efforts by fans and
		<a href="https://codeberg.org/godotsteam/godotsteam">GodotSteam</a> project.
		<br />
		<br />
		The macOS port is stable, unmodified, lightweight, ad-free, malware-free build that can be downloaded
		from this website for free by any player that owns BUCKSHOT ROULETTE in Steam.
		<br />
		<br />
		No assistance will be provided to players who are unable to confirm having the game purchased in their
		Steam library. The website automatically checks license after Steam sign-in and prevents unauthorized
		downloads. Sharing the build is considered piracy and therefore prohibited.
		<br />
		<br />
		<Button tag="a" class="block" href={new URL("download", PUBLIC_API_URL).href} rel="nofollow">
			Download for free (v1.2)
		</Button>
		<span class="mt-2 block text-xs leading-4 text-neutral-500">
			You must have BUCKSHOT ROULETTE purchased in Steam
		</span>
	</p>
	<div class="flex max-w-full shrink-0 flex-col gap-4 xs:max-w-40 sm:max-w-60 md:max-w-75">
		<img src={screenshot1} alt="The game in macOS window" width="300" height="186.5" />
		<img
			src={screenshot2}
			alt="The game in macOS window on macOS background"
			width="300"
			height="175"
		/>
		<img src={screenshot3} alt="The game in macOS Dock" width="300" height="262" />
	</div>
</div>
<Alert id="error-no-license" title="License not found">
	<p>
		Ensure you have <a href="https://store.steampowered.com/app/2835570/Buckshot_Roulette/">
			BUCKSHOT ROULETTE
		</a> purchased in Steam, your profile is public and your games library is open to everyone.
	</p>
</Alert>
<Alert id="error-license-unknown" title="License check failed">
	<p>
		Something went wrong while checking the game's license in your Steam library. Ensure your
		profile is public and your games library is open to everyone.
	</p>
</Alert>
<Alert id="success" title="Welcome">
	<p>
		You have been authorized and can now download the port using the "Download for free" button
		below. If you experience issues, ensure your browser allows cookies or contact port's author.
	</p>
</Alert>
<Alert id="try-again" title="Please try again">
	<p>
		Your Session was invalid so it was deleted. Please click the "Download for free" button again.
	</p>
</Alert>
<Modal id="auth" title="License verification">
	<p>You must sign in with either Steam or itch.io to prove you have purchased Buckshot Roulette</p>
	{#snippet buttons()}
		<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -->
		<a href={new URL("auth/steam", PUBLIC_API_URL).href} class="no-underline!">Steam</a>
		<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -->
		<a href={new URL("auth/itch.io", PUBLIC_API_URL).href} class="no-underline!">itch.io</a>
	{/snippet}
</Modal>
