# Bridge assist: Bedrock's placement help for Java players

Code: `bridge_assist.go`, called from the `use_item` case in `input.go`. Tests: `bridge_assist_test.go`.

## What Bedrock does

A Bedrock player who stands on a block and looks down past its edge at nothing still gets a block outline:
on the block they stand on. Using a block then places it against that block, on the side they look towards.
That is how Bedrock players bridge while walking forwards.

The Bedrock client does all of it. The server only gets an ordinary "use item on block" click with the
position of the block under the player and a horizontal face, so Dragonfly needs nothing special for Bedrock
players, and there is nothing to turn on or off for them.

A Java client has no such help. When its crosshair hits nothing it sends `use_item` (a click in the air)
instead of `use_item_on`, and nothing is placed.

## What Crossfly does for Java players

With the setting on, a `use_item` from a Java player who holds a block is checked: the session works out the
target a Bedrock client would have picked and calls `Player.UseItemOnBlock` with it, the same call a real
click makes. So everything a normal placement goes through still applies: `HandleItemUseOnBlock` and
`HandleBlockPlace` (plugins, build protection, map rules), the reach check, entities and players in the way,
game modes that cannot build, allow, deny and border blocks, the place sound and the item count. Nothing is
installed on the client.

If nothing came of it (no target, no room, cancelled), the `use_item` is handled as before
(`Player.UseItem`), so handlers that listen for a click in the air still get it.

The client did not expect the placement, so the session also sends it what a click of its own would have
shown: the block, the place sound, the arm swing and the new item count.

## The rules, and where they come from

1. Only when the crosshair hit nothing. The Java client decides that itself: it sends `use_item` only then.
   A `use_item` within 60 ms after a `use_item_on` is left alone, because a Java client sends both for one
   click when the block it hit passed the click on.
2. Only while looking down: the vertical part of the look direction is below -0.7, a pitch steeper than
   about 44.4 degrees.
3. The target is the first block straight below the player's eyes, at most 2 blocks down. For a standing
   player that is the block under the centre of their feet, up to 0.38 below them, so it still works at the
   start of a jump but not in a fall. The centre counts: a player who hangs over an edge with their centre
   past it has no target there.
4. The face is the horizontal side the player looks towards: east or west if the look direction points more
   along x, else south or north. Never a diagonal.
5. The click position is the point on top of the target block below the eyes. A slab used this way therefore
   lands as a top slab, level with the block the player stands on.

Sources:

- Rules 2 to 5 are the end of `GameRenderer::pick` in the public decompilation of an early Pocket Edition
  client (ReMinecraftPE, `source/client/renderer/GameRenderer.cpp`): if nothing was hit and `view.y < -0.7`,
  it clips from the player's position (the eyes in that version) to 2 blocks below, takes that hit, marks it
  as an indirect hit and sets the face from the larger of `view.x` and `view.z`.
- The current game still has the indirect hit: in the server binary (1.21.3 with symbols)
  `GameMode::continueBuildBlockAction` reads a flag of `HitResult` next to its type for it. The pick itself
  is client code and is not in the server binary, so the numbers (0.7, 2 blocks) are not confirmed for the
  current client.
- What players and Mojang's bug tracker say about the current game matches: MCPE-172103 (closed as working
  as intended: only the four sides of the block under the centre of the player, not diagonals, and not when
  the centre is over another block), MCPE-37389 (the block under the player is really clicked: a block with
  a use, like a furnace, is used instead; "45 degrees or less towards the ground"), and the Minecraft Wiki's
  bridging tutorial ("simple bridging", Bedrock only).

Guesses and known differences:

- The 0.7 and the 2 blocks come from the old client. They fit what is reported for the current one, nothing
  more.
- Blocks without a collision box (plants, torches) count as a whole block for the line down. Bedrock uses
  their outline shape, which Dragonfly does not have. Liquids are looked through.
- A Java client's crosshair reaches 4.5 blocks in survival, Bedrock's 5 with a mouse and more on touch, so a
  floor 4.5 to 5 blocks below counts as "nothing" on Java where a Bedrock client would have clicked it.
- As on Bedrock, the block under the player is clicked for real: standing on a chest or a crafting table and
  using a block this way opens it, unless the player sneaks.
- Only items that are blocks are assisted. Bedrock assists every item (a bucket, a spawn egg); those keep
  their Java meaning of a click in the air here.
- Bedrock also has "hold to keep building" (`GameMode::continueBuildBlock`: with the button held it goes on
  placing in a line as the player walks). That is not ported. A Java client that holds the use key clicks
  again every 4 ticks with a fresh crosshair hit. Looking down steeply (60 degrees or more) that works for
  bridging while walking: the crosshair is on the own block (nothing happens) or past the edge (assist).
  Looking down only 45 to 55 degrees the crosshair lands on top of the block just placed, and the next click
  is a normal Java click that puts a block on top of it. One click per block avoids that.

## The outline

Not added. The Java client draws the block outline itself, on its own crosshair hit only, and the server
cannot move it. A stand-in would be a display entity around the target block, shown while a Java player
holds a block and their crosshair hits nothing. It was left out because:

- it needs the client's crosshair hit on the server every tick for every Java player holding a block (a ray
  through blocks and entities with Java's shapes), where the assist itself costs nothing until a click;
- what Java can draw is not the thin black outline: a glowing block display shows a thick coloured glow
  that is also seen through walls, and item or block displays scaled to a frame need a resource pack to
  look right;
- it lags behind the crosshair by the round trip to the server, so it trails while the player turns or
  walks, exactly when bridging.

The placement itself is shown at once (block, sound, swing), which is the feedback that matters.

## Setting

`javasession.Config.BridgeAssist` (per `javasession.Run`, so per server). Crossfly's own server reads it
from `[Java] BridgeAssist` in `config.toml`. Off by default. The environment variable
`CF_JAVA_BRIDGE_ASSIST` overrides it for the process: `1` on, `0` off; servers with a main of their own can
be switched with it without a code change. The server logs "Bridge assist for Java players is on." at start
when it is.

With debug logging each assisted placement is logged ("bridge assist", block, face, eye, yaw, pitch).

## Testing

- `go test ./server/java/javasession -run BridgeAssist`: the target and face (slabs, chunk edges, negative
  coordinates, straight down, looking up, sneaking at an edge, jumps, out of reach), and placements through
  a real player (item count, a block already there, another player in the way, adventure mode, a handler
  that cancels, creative, slabs).
- Live: `go run ./server/java/cmd/dfjtest -bridgeassist -bridgetest` builds a short walkway in the air and
  puts each joining player on it with stone and slabs. Look down past an edge and use the block.
