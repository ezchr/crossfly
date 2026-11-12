# Warp pads

An administrator creates an area that teleports players when they walk into it, like the warp-pad plugins of Paper servers. This package is reusable by a Crossfly application; Crossfly Full also loads it directly.

Stand in the entrance and run:

```text
/warp create portal
```

Stand where the entrance should send players and run:

```text
/warp dest portal here
```

To shape the entrance:

```text
/warp edit portal
```

Nearby warp areas appear as glass for the editor alone. Break the glass to remove area blocks; hold ordinary glass and place against it to add area blocks. Glass placed away from other areas goes to the selected warp. The editor supplies glass when an empty hotbar slot is available. Real world blocks and held stacks are unchanged by area clicks. Walk-in teleports are suspended while editing. Run `/warp edit` again to finish.

Breaking a block shared by overlapping areas removes it from all of them. An area's final block cannot be removed; use `/warp delete` to delete it. Changes save immediately and other active editors refresh. Areas up to 50,000 blocks can be shaped by hand; larger cuboids use `/warp area`. Only loaded blocks within 48 blocks of the editor are previewed. Solid world blocks provide their own outline.

Other commands:

```text
/warp create <name> [x1 y1 z1 x2 y2 z2]
/warp area <name> [x1 y1 z1 x2 y2 z2]
/warp dest <name> here
/warp dest <name> <x y z> [yaw pitch]
/warp set <name>
/warp edit [name]
/warp require <name> <tag|none>
/warp deny <name> <tag|none>
/warp rank <name> <rank|none>
/warp message <name> <text|none>
/warp fade <name> on|off
/warp on <name>
/warp off <name>
/warp rename <name> <new-name>
/warp delete <name>
/warp list
/warp info <name>
/warp tp <name>
```

Coordinates support `~` offsets. Area coordinates are inclusive block coordinates; omitted corners select the block at your feet. Destinations preserve decimal coordinates and facing. `/warp set` creates a one-block pad with its destination at your current location, or changes an existing pad's destination. Names are case-insensitive and use letters, digits, underscores and hyphens.

Warps use insertion order for overlapping eligible triggers and a one-second player cooldown. Administrators bypass required ranks/tags; as on Paper, a deny-tag warp skips administrators. Fade applies 1.5 seconds of blindness and teleports after 0.3 seconds. A pending teleport is discarded after quitting, changing world, another teleport, entering edit mode, or when host rules prevent warping. Missing destination worlds do not fire.

Crossfly Full configuration:

```toml
[Warps]
Enabled = true
File = "warps.json"
```

The command requires an operator and runs in game. Disabling the feature adds no warp handler. Crossfly Core does not register the plugin. Warps are local to a backend, with named worlds `overworld`, `nether` and `end`; setting the destination in another world selects that world. JSON saves use a temporary file and atomic rename. Existing Paper coordinates are not imported automatically because the worlds need to correspond.

For a custom host, call `warps.Open(Config{...})`, then `Manager.Register()` at startup. Wrap each player's existing handler with `Manager.Wrap(p.Handler())`. Supply a stable world-name map, an administrator callback, and optionally tag, rank, rank-existence and eligibility callbacks. Without tag/rank callbacks, those restrictions cannot be configured from commands. The custom PvP application adapter uses its existing administrator/rank store and blocks walk-in warps while logged out, in combat, or in a minigame.

The engine's `player.SetBlockPreviews` API sends private block overlays through both Java and Bedrock sessions, reapplies them after chunk/subchunk delivery, and clears them on world changes. Preview clicks reach normal block handlers without harvesting, placing or consuming real blocks.
