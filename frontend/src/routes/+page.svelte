<script lang="ts">
	import mikeKlubnikaBadge from "$lib/assets/mike-klubnika-badge.gif";
	import screenshot1 from "$lib/assets/screenshot-1.avif";
	import screenshot2 from "$lib/assets/screenshot-2.avif";
	import screenshot3 from "$lib/assets/screenshot-3.avif";
</script>

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
		revolver with a 12-gauge shotgun.
		<br />
		<br />
		The game was published in Steam on April 4th, 2024 for Windows and SteamOS + Linux but never officially
		ported to macOS. Buckshot Roulette for macOS project attempts to fill that gap using the game's Godot
		engine to compile native game builds for Mac.
		<br />
		<br />
		Unlike the virtualized environments that emulate Windows, the native build has no visual glitches
		or audio issues and works using native Mac's graphics APIs.
		<br />
		<br />
		Porting is made possible thanks to <a href="https://godotengine.org/">Godot</a> multi-platform
		compilation, game's decompilation efforts by fans and
		<a href="https://codeberg.org/godotsteam/godotsteam">GodotSteam</a> project.
		<br />
		<br />
		The macOS port is stable, unmodified, lightweight, ad-free, malware-free build that can be downloaded
		from this website for free by any player that owns BUCKSHOT ROULETTE in Steam.
		<br />
		<br />
		No assistance will be provided to players who are unable to confirm having the game purchased in their
		Steam library. The website automatically checks license after Steam sign-in and protects the download
		link from unauthorized access. Sharing the build is prohibited and considered piracy.
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
