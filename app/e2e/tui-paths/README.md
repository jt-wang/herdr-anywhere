# Agent screens with picture paths

Each file is the raw output of one agent, recorded in a terminal 40 rows high and as many columns as its name says.
These widths are what the phone gets on common screens: 46 at 360 dp, 50 at 390–393 pt, 52 at 412 dp, 56 at 440 pt,
plus 40 (360 dp with larger text) and 66 (440 pt with smaller text). `e2e/page-touch.mjs` plays every file here, with
and without the agent's own links, and checks that each piece of a picture path is underlined and opens the path.

| File | Agent |
|---|---|
| `claude-code-2.1.322-*.ansi` | Claude Code 2.1.322 |
| `codex-0.167.1-*.ansi` | Codex 0.167.1 |
| `opencode-1.19.0-*.ansi` | OpenCode 1.19.0 |
| `pi-1.4.0-*.ansi` | Pi 1.4.0 |
| `kimi-code-2.5.2-*.ansi` | Kimi Code 2.5.2 |

How they were made:

- Kimi Code ran in a container with no network, no folders from the computer and no key. Its model was a small local
  server with fixed answers: it has Kimi Code read the two pictures one after the other, then says "Done.".
- Each agent ran in tmux with `TERM=xterm-256color`, `TERM_PROGRAM=ghostty` and `COLORTERM=truecolor`, with its update
  check off and a fresh config of its own.
- Every name is made up. The home is `/private/tmp/hx-sam/Photo Archive 2TB/work`, which has a space in a folder
  name. The agent runs in `garden-planner-mobile`. One picture is inside it and is named by a relative path,
  `screens/final-review/settings-dark-mode.png`. The other is in another project and is named by its full path,
  `weather-widget-companion-app/exports/release-candidate/home-screen.png`.
- The prompt asks the agent to look at both pictures and reply with one word. Claude Code sends the second one with
  its file tool, which lists the file in three columns: a mark, the path and its size. Kimi Code shows the full paths
  only in the prompt it repeats; its tool rows shorten them.
- The pictures hold random pixels for Claude Code, Codex, OpenCode and Kimi Code, and a colour gradient for Pi, which
  draws pictures inline.
- Model and provider names on screen are generic display names. Two changes were made to the bytes: the account
  plan after the model name in Claude Code's header is blanked with spaces of the same width, and the session id in Kimi Code's header has its
  letters and digits replaced with `x`.

To add an agent or a version, record it the same way, name the file `<agent>-<version>-<columns>.ansi`, and add its
paths to the test if they differ.
