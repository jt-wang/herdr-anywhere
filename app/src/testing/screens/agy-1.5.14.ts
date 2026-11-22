// The Antigravity CLI's (agy 1.5.14) screens, from `hc read` of 46- and 50-column terminals (a phone's width), with a
// made-up model behind its enterprise gateway in a scratch herdr-core session. Trimmed to the panel; paths made up.

// The first of two questions: a digit picks and goes on. "(Recommended)" is the model's, first.
export const Q1_46 = `
Question
──────────────────────────────────────────────

Question 1/2: Which color should the button
be?

> 1. (Recommended) Blue
  2. Red
  3. Write-in...

  ↑/↓ Navigate · → Next · enter Select · esc S
esc to cancel                       Fake Model
`
export const Q1_50 = `
Question
──────────────────────────────────────────────────

Question 1/2: Which color should the button be?

> 1. (Recommended) Blue
  2. Red
  3. Write-in...

  ↑/↓ Navigate · → Next · enter Select · esc Skip
esc to cancel                           Fake Model
`

// The second, multi-select: a digit ticks a box (the cursor goes to it), Enter sends every answer.
export const Q2_46 = `
Question
──────────────────────────────────────────────

Question 2/2: Which sizes should it come in?

> 1. [ ] Small
  2. [ ] Large
  3. Write-in...

  ↑/↓ Navigate · ← Back · space Toggle · enter
esc to cancel                       Fake Model
`
export const Q2_50 = `
Question
──────────────────────────────────────────────────

Question 2/2: Which sizes should it come in?

> 1. [ ] Small
  2. [ ] Large
  3. Write-in...

  ↑/↓ Navigate · ← Back · space Toggle · enter Sub
esc to cancel                           Fake Model
`

// The same, one box ticked.
export const Q2_TICKED_46 = `
Question
──────────────────────────────────────────────

Question 2/2: Which sizes should it come in?

> 1. [x] Small
  2. [ ] Large
  3. Write-in...

  ↑/↓ Navigate · ← Back · space Toggle · enter
esc to cancel                       Fake Model
`
export const Q2_TICKED_50 = `
Question
──────────────────────────────────────────────────

Question 2/2: Which sizes should it come in?

> 1. [x] Small
  2. [ ] Large
  3. Write-in...

  ↑/↓ Navigate · ← Back · space Toggle · enter Sub
esc to cancel                           Fake Model
`

// Write-in's digit opens its answer: the text, then Enter.
export const WRITE_IN_46 = `
Question
──────────────────────────────────────────────

Question 2/2: Which sizes should it come in?

  1. [x] Small
  2. [ ] Large
> 3. Write-in...

  Your answer:




  enter Submit All · esc Back
esc to cancel                       Fake Model
`
export const WRITE_IN_50 = `
Question
──────────────────────────────────────────────────

Question 2/2: Which sizes should it come in?

  1. [x] Small
  2. [ ] Large
> 3. Write-in...

  Your answer:




  enter Submit All · esc Back
esc to cancel                           Fake Model
`

// A command to approve: a digit acts at once; No, cancel ends the turn and agy asks at its prompt.
export const RUN_46 = `
Command
──────────────────────────────────────────────

Requesting permission for:
   echo checking; echo 'one test failed'
>&2; exit 3

Run this command?
> 1. Yes, run command
  2. Yes, and always allow in this
conversation for commands that start with
'echo'
  3. Yes, and always allow for commands that
start with 'echo' (Persist to settings.json)
  4. No, cancel

  ↑/↓ Navigate · tab Amend · ctrl+g edit/expan
esc to cancel                       Fake Model
`
export const RUN_50 = `
Command
──────────────────────────────────────────────────

Requesting permission for:
   echo checking; echo 'one test failed'
>&2; exit 3

Run this command?
> 1. Yes, run command
  2. Yes, and always allow in this conversation
for commands that start with 'echo'
  3. Yes, and always allow for commands that start
with 'echo' (Persist to settings.json)
  4. No, cancel

  ↑/↓ Navigate · tab Amend · ctrl+g edit/expand co
esc to cancel                           Fake Model
`

// A file to create.
export const CREATE_46 = `
Create file
──────────────────────────────────────────────

/work/acme/hello.txt  +
  1  +  hello
  2  +  world
Reason: outside workspace

Allow creation of this file?
> 1. Yes, allow creation
  2. Yes, and always allow non-workspace
access
  3. No, deny creation

  ↑/↓ Navigate · f full diff
esc to cancel                       Fake Model
`
export const CREATE_50 = `
Create file
──────────────────────────────────────────────────

/work/acme/hello.txt  +2
  1  +  hello
  2  +  world
Reason: outside workspace

Allow creation of this file?
> 1. Yes, allow creation
  2. Yes, and always allow non-workspace access
  3. No, deny creation

  ↑/↓ Navigate · f full diff
esc to cancel                           Fake Model
`

// An edit to accept.
export const EDIT_46 = `
Pending edit
──────────────────────────────────────────────

/work/acme/notes.txt  +
  1     a
  2  +  b
  3  +  c

  shift+tab to auto-approve file edits
Accept this file edit?
> 1. Yes, accept this change
  2. No, reject this change

  ↑/↓ Navigate · f full diff
esc to cancel                       Fake Model
`
export const EDIT_50 = `
Pending edit
──────────────────────────────────────────────────

/work/acme/notes.txt  +2
  1     a
  2  +  b
  3  +  c

  shift+tab to auto-approve file edits
Accept this file edit?
> 1. Yes, accept this change
  2. No, reject this change

  ↑/↓ Navigate · f full diff
esc to cancel                           Fake Model
`

// /artifact on a plan written for review: y approves (agy carries it out), n rejects. Over it, the prompt.
export const REVIEW_46 = `
──────────────────────────────────────────────
>
──────────────────────────────────────────────
Action required (1 left)
› □ new implementation_plan.md   open  approve

Keyboard: ↑/↓ Navigate  y/n Approve/reject
  shift+a Approve all  p Preview
  ctrl+g open in editor  esc Done

                                    Fake Model
`
export const REVIEW_50 = `
──────────────────────────────────────────────────
>
──────────────────────────────────────────────────
Action required (1 left)
› □ new implementation_plan.md   open  approve rej

Keyboard: ↑/↓ Navigate  y/n Approve/reject
  shift+a Approve all  p Preview
  ctrl+g open in editor  esc Done

                                        Fake Model
`
