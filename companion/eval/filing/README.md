# Evaluating the filing agent

The progress page relies on an AI agent on your computer, the filing agent, to sort what you say in your panes into tasks, and to decide when a stopped pane's task is done or waiting on you. A change to its rules, to the commands it uses, or a new model can quietly make it worse. This evaluation shows whether it still does what the page promises, in each of the app's languages.

It runs the shipped path end to end: the companion queues a batch, starts the filing pane in herdr-core, and types the batch in, as `herdr-anywhere serve` does. The agent works with your own login or key. Nothing goes to us.

## Run it

```sh
companion/eval/filing/run.sh                    # Claude Code with Haiku 5.5, all seven languages
LANGS="ja ko" companion/eval/filing/run.sh
AGENT="pi --model <model>" LABEL="Pi · <model>" companion/eval/filing/run.sh
```

You need herdr-core, Go, Python 3 and the agent, logged in or with its key in the environment (`ANTHROPIC_API_KEY` for Claude Code). The run uses a herdr-core session of its own, so your own panes are untouched. Claude Code keeps its login in your home folder, so it runs with your `HOME`, reads only the eval folder's settings (`--setting-sources project`), and `~/.claude.json` records each eval folder as trusted. Pi runs commands without asking, so it gets a home folder of its own for the run and reads only `~/.pi/agent` of yours. Codex reads its login from `~/.codex`, runs in its sandbox, and its session records from the run are moved out of `~/.codex/sessions` into the run's folder. At the end, `score.py` prints each rule and writes `report.json`.

To compare agents and models, list them in a file, one per line: a name, a tab, the command. `matrix.sh LIST OUT-DIR` runs each and prints one table. Only the names and the agent's program go into the reports, so a report can be shared without showing how your computer reaches its models.

The agent you pick in the app needs to report its status to herdr-core, so the companion knows when the filing pane is free: Claude Code, Pi and Kimi Code do; Codex from 0.163.

## Results

Each agent with the model named, all seven languages. Haiku 5.5 ran on the filing rules as they are now, with all thirteen checks; the others ran before R10–R13 were added, on the nine checks there were then. The table is a snapshot: run `matrix.sh` for your own.

| Agent · model | Rules passed | Seconds per language |
|---|---|---|
| Claude Code · Haiku 5.5 (the default) | 13 of 13 | 79 |
| Claude Code · Sonnet 5.5 | 9 of 9 | 34 |
| Codex · GPT-6-Astra | 9 of 9 | 43 |
| Pi · DeepSeek V4.1 Flash (three runs) | 9 of 9 each time | 38–48 |
| Pi · GLM 5.3 Flash | 9 of 9 | 83 |

What got them all there: the filing agent reads the batch with one command and answers it with one more, a JSON document in a quoted here-document. Before, it placed each item with its own command, and the shell changed "$29" in a question, and agents that run commands without asking went looking around the computer.

Tried another agent or model? Run it and open an issue with its `report.json`; how it did on your own work is just as welcome.

## What it checks

`cases.json` (made by `make_cases.py`) holds the same made-up scene for each language, with the phone set to that language. In it: 8 panes that stop, 6 sessions found when the page is turned on (one of them long, two from panes working on one thing), 4 messages typed into panes, and after the batch, a correction the user types into the filing pane. Most of what the user and the agents say is in the phone's language. Two items are in another language, English (Chinese for the English phone), and the agent must still write in the phone's.

| Rule | What it means | Passes when |
|---|---|---|
| R1 | A pane that asks the user something is never marked done or still going | every asking pane is `waiting`, with what it asks |
| R2 | A stopped pane's task gets the right stage: done, waiting or doing | 90% of stops, in each language |
| R3 | Titles and questions are in the phone's language: Simplified and Traditional Chinese apart, Spanish apart from English | every line it wrote |
| R4 | Names, numbers, file names and prices stay as they were (`README`, `8080`, `$29`) | every one |
| R5 | Nothing is left unsorted | the queue is empty in each language |
| R6 | The agent runs only `herdr-anywhere track queue/place/tree/set/move`, reads no more than its own rules, and edits no files (checked for Claude Code, Pi and Codex, from their session records) | every command |
| R7 | A session found at the start gives the tasks in it, done or not, and no more | 2 of 3 sessions, in each language |
| R8 | A message goes on the task it is about, or on a new one when it is new | 3 of 4 messages, in each language |
| R9 | Small talk makes no task | every language |
| R10 | In a long session, each thing gets the stage the agent's answers show, wherever it sits in the session | 4 of 5, in each language |
| R11 | Two panes working on one thing no task names yet: their tasks go under one task | every language |
| R12 | The user tells the filing pane a task is finished, and it marks it done | every language |
| R13 | Scanning a session again (after an update), a task already in the tree is found, not made twice, and takes the stage | every language |

`test_score.py` checks the scorer itself without a model: a made-up run that does everything right passes, and each way of getting it wrong fails its rule.

## Is the last run still current?

The Tests workflow no longer runs this check, so a release doesn't wait on it; run it by hand. `check.sh` passes only when `last-report.json` comes from a passing run on the filing rules, the `track` code and this evaluation as they are now (a hash of those files). After changing any of them, run `run.sh` again and copy its `report.json` to `last-report.json`.
