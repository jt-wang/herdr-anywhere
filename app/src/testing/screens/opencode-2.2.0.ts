// OpenCode 2.2.0's screens, from `hc read` of a 46-column terminal (a phone's width), with a made-up model in a
// scratch herdr-core session. Trimmed to the prompt's box.

// A command to allow: a row of three, no numbers.
export const PERMISSION = `
  ┃  △ Permission required
  ┃
  ┃  $ touch made-1.txt
  ┃
  ┃
  ┃   Allow once   Always allow   Reject
  ┃
  ┃  ctrl+f fullscreen  ⇆ select  enter confir
  ┃
`

// One question: a digit sends it.
export const SINGLE = `
  ┃  Questions
  ┃
  ┃  Which color should the demo button
  ┃  be?
  ┃
  ┃  1. Teal (Recommended)
  ┃     Matches the demo logo
  ┃  2. Orange
  ┃     Stands out on white
  ┃  3. Gray
  ┃     Quiet, fits any page
  ┃  4. Type your own answer
  ┃
  ┃  ↑↓ select  enter submit  esc dismiss
  ┃
`

// The first of two questions.
export const TWO_Q1 = `
  ┃  Questions
  ┃
  ┃  Color  Size  Submit
  ┃
  ┃  Which color should the demo button
  ┃  be?
  ┃
  ┃  1. Teal (Recommended)
  ┃     Matches the demo logo
  ┃  2. Orange
  ┃     Stands out on white
  ┃  3. Gray
  ┃     Quiet, fits any page
  ┃  4. Type your own answer
  ┃
  ┃  ⇆     ↑↓       enter       esc
  ┃  tab   select   confirm     dismiss
  ┃
`

// The second, multi-select, two ticked.
export const TWO_Q2 = `
  ┃  Questions
  ┃
  ┃  Color  Size  Submit
  ┃
  ┃  How big should it be?
  ┃
  ┃  1. [✓] Small
  ┃         32 px tall
  ┃  2. [✓] Large
  ┃         48 px tall
  ┃  3. [ ] Type your own answer
  ┃
  ┃  ⇆     ↑↓       enter       esc
  ┃  tab   select   toggle      dismiss
  ┃
`

// Its Submit page, after Tab.
export const TWO_SUBMIT = `
  ┃  Questions
  ┃
  ┃  Color  Size  Submit
  ┃
  ┃  Color: Gray
  ┃  Size: Small, Large, Medium too
  ┃
  ┃  ⇆ tab  enter submit  esc dismiss
  ┃
`
