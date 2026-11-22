# KBTU Golang Checkpoints

A Go command-line program for working with a music library, and since
Checkpoint 2, for controlling a networked music player over its Wi-Fi
remote-control protocol. Every piece of work is a **task**: tasks are created, validated,
stored in memory, and processed concurrently by a fixed **worker pool** fed
through a bounded queue.

Requires **Go 1.27**. The Go module is in `src/`, so run commands from there.

## 1. Purpose and task processing

Music-library tasks (Checkpoint 1, unchanged):

| Task | What it computes |
| --- | --- |
| `analyze_library` | track, artist, album and genre counts, missing metadata, duplicate groups |
| `generate_playlist` | a scored playlist within a duration and track limit, by genre and mood |
| `discover_similar_tracks` | the tracks most similar to a reference track |

Player tasks (`player_control`, new in Checkpoint 2) send one player command
and **confirm the effect by reading the player back**. The player's replies
don't prove a command worked, so a task is only completed after the readback
matches:

| Action | Confirmed when |
| --- | --- |
| `status` | model, volume, play state, play mode, progress and now-playing were read |
| `set_volume` | `getVolume` reads the requested value (absolute, or relative and clamped) |
| `pause` / `resume` / `toggle` | the play state changes and progress freezes or advances |
| `next` / `previous` | a different file plays and progress restarts |
| `restart` | the same file plays again from the start |
| `seek` | progress lands on the target and continues from there |
| `set_play_mode` | `getPlayMode` reads the requested mode |
| `library` | a page of the player's own library was read |
| `play_track` | the chosen library track is the one playing |

Safety rules are checked **before anything is sent**, and a refused command
fails the task without touching the player:
- volume never goes up by more than 5 steps at once, and never above the player's maximum
- seeks are limited to FLAC/MPEG tracks and stay away from the end of the track
- pause and resume only use the two reviewed command bytes
- `play_track` only plays plain files from the all-tracks list

## 2. Creating and submitting tasks

```
Create(input)      -> validates input, assigns a unique ID (task-0001 ...), stores it as Pending
Submit(id)         -> Pending -> Queued, puts the ID into the queue, returns immediately
CreateAndSubmit()  -> both in one call
Get(id) / List()   -> safe copies of tasks, at any time, also while workers run
```

`Submit` never waits for processing. The synchronous `TaskService.Process(id)`
from Checkpoint 1 still exists for Pending tasks.

## 3–5. Worker pool, worker count, queue capacity

```
             CreateAndSubmit / Submit
                       |
                       v
   +-------- bounded queue: chan string (task IDs) --------+
   |   capacity 16 (-queue N)                              |
   +-------------------------------------------------------+
          |                 |                 |
          v                 v                 v
      worker 1          worker 2          worker 3    (-workers N, default 3)
          |                 |                 |
          +---------+-------+---------+-------+
                    v                 v
     Memory store (RWMutex)      Processor
     Queued -> Processing ->     music tasks run in parallel;
     Completed / Failed          player tasks share one session
```

- `service.StartPool(tasks, workers, capacity)` starts exactly `workers`
  goroutines (default **3**). Each runs `for id := range queue`, so it stays
  alive for the next task and is never created per task.
- The queue is a buffered channel of task IDs with capacity **16** by default
  (`-queue N`). It never grows.
- Tasks can finish in any order. Nothing depends on completion order.

## 6. When the queue is full

**Submission is rejected** with `domain.ErrQueueFull`. The task stays
**Pending**: it is never marked Queued or Processing, and it can be submitted
again later. The CLI prints the rejection; to see it, run
`go run ./cmd/setlist -player 127.0.0.1 -queue 2 -workers 2` (with the fake
player from section 11 running).

`Submit` checks capacity and sends while holding the pool mutex, and workers
only ever *receive*. So a free slot can't disappear between the check and the
send, and the send never blocks.

## 7. Task lifecycle

```
            Submit               worker takes it
Pending ------------> Queued -------------------> Processing --> Completed
   |                                                   |
   +--- Process (sync, CP1) -------------------------->+-------> Failed
```

| From | Allowed to |
| --- | --- |
| Pending | Queued (submit), Processing (synchronous `Process`) |
| Queued | Processing (only a worker, via `StartQueued`) |
| Processing | Completed (valid result stored) or Failed (error stored, no result) |
| Completed / Failed | nothing: final states, no retries |

Any other transition is an error, and the task is left unchanged:
- submitting a Queued, Processing, Completed or Failed task
- synchronously processing a Queued task
- starting the same task twice

Each check-and-change happens under one store lock, so two workers can never
both move a task to Processing.

## 8. Shared data and synchronization

| Shared data | Protected by | Why |
| --- | --- | --- |
| task map + order (`storage.Memory`) | `sync.RWMutex` | many readers (`Get`/`List`), writes are atomic check-and-set; tasks are deep-copied on the way in and out, so no caller shares mutable fields |
| sample/imported library (`domain.Library`) | `sync.RWMutex` | workers read it concurrently |
| task ID counter | `atomic.Uint64` | concurrent `Create` calls get unique IDs |
| queue + `stopped` flag (`service.Pool`) | `sync.Mutex` + channel ownership | the pool is the only sender and the only closer |
| one player session (`player.Controller`) | `sync.Mutex` (control lock) | a control is several RPCs (read baseline → set → read back); two player tasks must not interleave |
| player data connection (`session.Session`) | `sync.Mutex` (RPC lock) | one request in flight, so a reply is never matched to the wrong request |
| heartbeat connection | owned by one goroutine | no lock needed; nothing else touches it |

- No lock is held while a task is processed. Music tasks run in parallel even
  while a player task is waiting for the device.
- Lock order is always pool → store and controller → session. Nothing takes
  them in the opposite order, so they can't deadlock.

## 9. Stopping the workers

`Pool.Shutdown()`:
1. takes the pool mutex, sets `stopped`, and closes the queue channel. Every
   send happens under the same mutex after checking `stopped`, so there is no
   send on a closed channel.
2. waits on a `sync.WaitGroup`. Workers keep draining the closed channel, so
   **every accepted task is still processed**, then their `range` loops end.
3. Later or concurrent `Shutdown` calls are a no-op (`sync.Once`) and also
   return only after the workers are done.

After shutdown, `Submit` returns `domain.ErrPoolStopped` and the task stays
Pending. The CLI shuts the pool down first and then closes the player
controller. Closing the session stops its heartbeat goroutine and waits for it.

The only waits in the program are the player's readback polling and its
heartbeat timer; both are part of the protocol. No sleeps are used to make
anything look concurrent.

## 10. Running and testing

```sh
cd src
go run ./cmd/setlist                       # sample library, 3 music tasks through the pool
go run ./cmd/setlist -library /path/music  # import a real directory (needs ffprobe on PATH)
go run ./cmd/setlist -workers 2 -queue 4   # change pool size and queue capacity

gofmt -l .            # prints nothing
go vet ./...
go test ./...
go test -race ./...
```

What the tests cover:
- `internal/service/pool_test.go`:
  - pool startup and configuration errors
  - Submit returning before processing finishes
  - many tasks, each with its own correct result
  - **two tasks inside the processor at the same time**
  - peak concurrency equal to the worker count
  - Failed state on processing errors
  - duplicate and finished submissions rejected
  - full queue (task stays Pending)
  - reads during processing
  - shutdown draining accepted tasks
  - submit after shutdown rejected
  - double shutdown
  - Submit racing Shutdown without losing an accepted task
- `internal/storage`: an atomic Enqueue under 50 concurrent attempts, and queued-only start.
- `internal/playerlink`:
  - byte-exact framing against an archived real request
  - codec, CRC, reassembly and corruption
  - RPC poisoning and argument refusal
  - session open, heartbeat, failure and idempotent close
- `internal/player`: every control's confirm, refuse and not-confirmed paths, plus controller serialization and reopening a lost session.

## 11. Player control

`internal/playerlink` is a Go port of the core of a Python reference
implementation of the player's protocol (kept outside this repository):
- `protocol`: CRC, signature codec, messages, frames and reassembly
- `rpc`: the reviewed operation allowlist and a client that becomes unusable after any failed exchange
- `session`: model check, a pipelined heartbeat on its own goroutine, an RST close

`internal/player` ports the control logic, including albums and cover art.
Not ported:
- notification-based activation
- Bluetooth
- discovery
- automatic reconnect supervision (the controller simply opens a new session on the next task)
- evidence recording
- the web control page

The fake player stays in the Python reference project. To try everything
without a player, start its `tools.fake_player` module from that project's
parent folder. It listens on `127.0.0.1:4001` (data) and `:4002` (heartbeat).

Then, in `src/`:

```sh
go run ./cmd/setlist -player 127.0.0.1
go run ./cmd/setlist -player 127.0.0.1 -player-actions status,next,previous
PLAYER_TARGET=127.0.0.1 go test -race -run Integration -v ./internal/player/
```

The fake's state resets on every new connection.

`-player-actions` takes a comma-separated list. The default is
`status,volume-down,mode:2,play-track:1,seek:30000,toggle,library:5`.

| Token | Task |
| --- | --- |
| `status` | read everything |
| `volume-up`, `volume-down`, `volume:N` | relative volume (`volume:-3`) |
| `volume-set:N` | absolute volume |
| `pause`, `resume`, `toggle` | play state |
| `next`, `previous`, `restart` | track navigation |
| `seek[:MS]` | seek, default 30000 ms |
| `mode[:N]` | 1 in order, 2 repeat all (default), 3 repeat one, 4 shuffle |
| `play-track[:N]` | play library position N (default 0) |
| `library[:N]` | first N library tracks (default 10) |

Player tasks run in whatever order the workers pick them up, and each one
confirms its own effect. Give a single action per run when the order matters.

Other flags: `-player-data-port` (default 4001) and `-player-heartbeat-port` (default 4002).

### Terminal remote (`tui`): a fun extra, not part of Checkpoint 2

`cmd/tui` is an interactive remote for the player, built with
[Bubble Tea](https://github.com/charmbracelet/bubbletea) and Lip Gloss. It
uses the same `player.Controller` as the tasks, so every action is a
confirmed control and the same safety rules apply. The interface is centred in
the terminal and has three tabs: **Now playing**, **Tracks** and **Albums**.

```sh
go run ./cmd/tui -player 127.0.0.1    # fake player
go run ./cmd/tui -player $PLAYER      # real player
```

**Album cover.**
- After each track change the remote waits about 2.5 s, then reads the
  cover with `getCover`. Right after a switch the player can still return no
  image or the old one.
- The image is drawn as 24×12 cells, two pixels per cell, using `▀` with the
  top pixel as the foreground colour and the bottom pixel as the background.
- Covers come back stored bottom-up (confirmed live by the Python reference
  project), so the remote flips them vertically. It
  also crops them to a centred square.
- When there is no cover, or the image can't be decoded, a placeholder is shown.

**Library and albums.**
- On first start the remote reads every track, then the album list, with a
  progress bar (`tracks 1200/4399 · 27%`). Each 50-row page is a separate
  request, so status updates and keys keep working while it loads.
- Everything is saved to `player-library-<host>.json` in the current directory
  (`-library-cache FILE` to change it), and later starts load that file
  instantly. A saved file without albums only reads the album list. `R` reads
  everything from the player again.
- The file lists your songs and their paths on the player, so `.gitignore`
  excludes it.
- Saved tracks keep their player locator, so a track plays straight away
  without paging through the library, wherever it sits.
- Albums come from the player's own album view (`getPlaylistLists` on
  `1#1@[05]`, ported from the Python research). `enter` opens an album; its
  tracks are read once and saved too.
- Playing from inside an album uses the album as the player's queue, so
  next/previous stay in that album.
- Playing the track that is already playing reports "already playing": the
  player doesn't restart it.

**Search.** `/` filters the current list as you type: tracks, albums, or an
open album's tracks. From Now playing it opens Tracks. Every word must appear
in the title or artist (or album name), in any case. `enter` keeps the filter,
`esc` clears it.

**Volume.**
- `v` lets you type a volume, and clicking the volume bar sets one too.
- Going up, the remote raises it in steps of at most 5, confirming each step
  by readback, so you never get a sudden loud jump. Going down happens in one
  step.
- `+`/`-` and the mouse wheel on Now playing change it by one.

**Mouse.**
- Click a tab to switch to it.
- Click a list row to select it, and click it again to play or open it.
- Click the progress bar to seek there.
- Click the volume bar to set the volume.
- The wheel scrolls lists, and changes volume on Now playing.

| Key | Action |
| --- | --- |
| `space` | play / pause |
| `n` / `p` | next / previous track |
| `r` | restart the current track |
| `←` / `→` | seek 10 s back / forward |
| `+` / `-` | volume up / down by one step |
| `v` | type a volume |
| `m` | cycle play mode |
| `/` | search the current list |
| `tab` (or `l`) | next tab |
| `↑`/`↓` or `k`/`j` | move in a list |
| `pgup`/`pgdn`, `g`/`G` | page up / down, first / last row |
| `enter` | play the track / open the album |
| `esc` / `backspace` | clear the search, or leave an open album |
| `R` | read the library and albums from the player again |
| `q` | quit (`ctrl+c` also works while typing) |

- The status (track, progress, volume, mode) is read every `-refresh`
  (default 1 s), but never while a command or a cover read is running.
- Only one command runs at a time; a key pressed meanwhile gets a "still
  waiting" message.
- It also takes `-player-data-port` and `-player-heartbeat-port`.

**Slow Wi-Fi.** The player's radio is slow and lossy: the Python research
measured about 1 s average and up to 5 s ping, and this run saw the same. So
the defaults follow the Python control page:

| Setting | Value |
| --- | --- |
| RPC timeout | 12 s |
| Heartbeat | every 1 s, failing after 10 s without an answer |
| Wait for a track change to confirm | up to 15 s |

On Linux the sockets also use `TCP_THIN_LINEAR_TIMEOUTS`, so retransmissions
don't back off past the player's 5 s heartbeat deadline. The player appears to
serve one remote-control client at a time: if another remote or the
player's phone app is connected, a new session times out on its first request.

To see real covers with the fake player, the bridge serves the covers captured
from the device (kept in the Python reference project) by default (still bottom-up, as
the device sends them). Use `--cover FILE.jpg` (repeatable) to serve your own,
or `--synthetic-covers` to keep the fake's non-image placeholders.

### Testing on a real player

1. Put the player on the same Wi-Fi as the computer and switch its Wi-Fi
   remote control on in its settings. Note its IP address.
2. From `src/`, run each command and check that its task ends `completed`:

```sh
export PLAYER=192.168.x.y
go run ./cmd/setlist -player $PLAYER -player-actions status       # read-only check
go run ./cmd/setlist -player $PLAYER -player-actions library:5
go run ./cmd/setlist -player $PLAYER -player-actions volume-down
go run ./cmd/setlist -player $PLAYER -player-actions toggle       # pause
go run ./cmd/setlist -player $PLAYER -player-actions toggle       # resume
go run ./cmd/setlist -player $PLAYER -player-actions next
go run ./cmd/setlist -player $PLAYER -player-actions previous
go run ./cmd/setlist -player $PLAYER -player-actions seek:30000
go run ./cmd/setlist -player $PLAYER -player-actions mode:2
go run ./cmd/setlist -player $PLAYER -player-actions play-track:0
go run ./cmd/setlist -player $PLAYER                              # full concurrent demo
PLAYER_TARGET=$PLAYER go test -race -run Integration -v ./internal/player/
go run ./cmd/tui -player $PLAYER                                 # remote: reads + saves the library once
```

The integration test restores volume, play mode and play state at the end. It
does change the current track, through `play_track`.

If the status check fails, check that the Python reference project's
`tools.read_device` module can read the same device (one `getModelNumber`
read into a new output folder).
