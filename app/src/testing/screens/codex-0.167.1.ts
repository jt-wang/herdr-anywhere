// Codex 0.167.1's screens, from `hc read` of a 46-column terminal (a phone's width), with a made-up model in a scratch
// herdr-core session. Trimmed to the lines of the prompt.

// A command to approve.
export const CMD = `
  Would you like to run the following
  command?

  Environment: local

  Reason: Make a marker file outside the
  sandbox?

  $ touch made-1.txt


› 1. Yes, proceed (y)
  2. Yes, and don't ask again for commands
     that start with \`touch made-1.txt\` (p)
  3. No, and tell Codex what to do differently
     (esc)

  Press enter to confirm or esc to cancel
`

// A change to approve.
export const PATCH = `
  Would you like to make the following
  edits?

  Description: Apply proposed file edits
  Destination:
  /work/fake-lab/projects/src/outside-4.txt


› 1. Yes, proceed (y)
  2. Yes, and don't ask again for these files
     (a)
  3. No, and tell Codex what to do differently
     (esc)

  Press enter to confirm or esc to cancel
`

// Plan mode's request_user_input, first of two questions: each option's description in a column of its own.
export const ASK1 = `
  Question 1/2 (2 unanswered)
  Which color should the demo button be?

  › 1. Teal (Recommended)  Matches the demo
                           logo
    2. Orange              Stands out on
                           white
    3. Gray                Quiet, fits any
                           page
    4. None of the above   Optionally, add
                           details in notes
                           (tab)

  tab to add notes | enter to submit answer
  ←/→ to navigate questions
  esc to interrupt
`

// The second question.
export const ASK2 = `
  Question 2/2 (1 unanswered)
  How big should it be?

  › 1. Small              32 px tall
    2. Large              48 px tall
    3. None of the above  Optionally, add
                          details in notes
                          (tab)

  tab to add notes | enter to submit all
  ←/→ to navigate questions
  esc to interrupt
`

// The end of a plan: Codex reports done and waits on this.
export const PLAN = `
  # Demo plan

  • Paint the demo button teal
  • Check it on a phone


  Worked for <1s • 11:05


  Implement this plan?


› 1. Yes, implement this plan
  2. Yes, clear context and implement
  3. No, stay in Plan mode

  enter select · esc back
`

// An ordinary done: the prompt.
export const DONE = `


› Ask Codex to do anything

  Fake Model default · /work/fake-lab/projects/s…
  ← for agents · ? for shortcuts
`
