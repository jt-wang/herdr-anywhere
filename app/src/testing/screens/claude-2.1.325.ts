// Claude Code 2.1.325's screens, from `hc read` of a 46-column terminal (a phone's width), with a made-up model in a
// scratch herdr-core session. Trimmed to the lines under the prompt.
export const RULE46 = '─'.repeat(46)
export const MULTI = `
❯ QMULTI please
${RULE46}
←  ☒ Plan  ☒ Pages  ✔ Submit  →

Which pages should get the new banner?

❯ 1. [✔] Home
         The first page people see
  2. [ ] Cart
         Where people pay
  3. [✔] Help
         Questions and answers
  4. [ ] Type something
     Submit
${RULE46}
  5. Chat about this

Enter to select · Tab/Arrow keys to navigate ·
Esc to cancel
`

export const REVIEW = `
❯ QMULTI please

${RULE46}
←  ☒ Plan  ☒ Pages  ✔ Submit  →

Review your answers

 ● Which plan should the fake shop use?
   → Wait for the full rewrite
 ● Which pages should get the new banner?
   → Home, Help

Ready to submit your answers?

❯ 1. Submit answers
  2. Cancel
`

export const PLAN = `
▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔

  ──────────────────────────────────────────
   Exit plan mode?

    Claude wants to exit plan mode

    ❯ 1. Yes, and switch to default (ask
         each time) for this session
      2. No
`

// Long labels in Chinese wrap with no sign of where the label ends: the screen can't be trusted for the words, only
// for the numbers (the card takes the words from the record).
export const LONG = `
❯ QLONG now
${RULE46}
 ☐ 横幅

假商店的横幅这周先做哪一种，下周再补另一种？

❯ 1. 先只改首页那一个文件，把横幅放到最上面，
     颜色先用深绿色，别的页面这周都不动（推荐
     ）
     半小时能做完，改坏了也只影响首页这一处地
     方
  2. Redo the banner on every page of the fake
     shop at once so that they all look the
     same from the start
     Takes about two weeks and every page of
     the shop changes
  3. 先不改
     代码不动
  4. Type something.
${RULE46}
  5. Chat about this

Enter to select · ↑/↓ to navigate · Esc to
cancel
`


// The first question of the same call.
export const Q1 = `
❯ QMULTI please
──────────────────────────────────────────────
←  ☐ Plan  ☐ Pages  ✔ Submit  →

Which plan should the fake shop use?

❯ 1. Ship the small fix now (Recommended)
     One file changes; nothing else moves
  2. Wait for the full rewrite
     Two weeks more, every page changes
  3. Only tell the team
     No change to the code
  4. Type something.
──────────────────────────────────────────────
  5. Chat about this

Enter to select · Tab/Arrow keys to navigate ·
Esc to cancel
`

// One question, one choice.
export const QONE = `
❯ QONE now
──────────────────────────────────────────────
 ☐ Color

Which color should the fake button be?

❯ 1. Green
     Calm and easy to read
  2. Orange
     Loud, stands out
  3. Type something.
──────────────────────────────────────────────
  4. Chat about this

Enter to select · ↑/↓ to navigate · Esc to
cancel
`

// A permission, its long options wrapped.
export const TOUCH = `
❯ TOUCH a file

⏺ Making an empty file
  ⎿  $ touch made-by-fake.txt

──────────────────────────────────────────────
 Bash command
 Tip: auto mode handles these prompts for you
 — choose "switch to auto mode" below
 Make an empty file
╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌
 touch made-by-fake.txt
╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌
 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and always allow access to
      /work/fake-lab/projects/src/fake-shop
      from this project
   3. Yes, and switch to auto mode · auto
      mode handles these prompts for you
   4. No

 Esc to cancel · Tab to amend
`

// The second question after its own answer was typed on its row (the arrows, then the text and Enter): the row
// takes the words, unticked.
export const OWN_TYPED = `
❯ QMULTI please
${RULE46}
←  ☒ Plan  ☐ Pages  ✔ Submit  →

Which pages should get the new banner?

  1. [ ] Home
         The first page people see
  2. [ ] Cart
         Where people pay
  3. [ ] Help
         Questions and answers
❯ 4. [ ] Footer row
     Submit
${RULE46}
  5. Chat about this

Enter to select · Tab/Arrow keys to navigate ·
ctrl+g to edit in Vim · Esc to cancel
`

// Then Enter: the row ticked, the cursor still on it. Tab from here only goes down to Submit (below); from an option's
// row it goes to the review. Read off Claude Code 2.1.325 at 46 columns, a made-up model.
export const OWN_TICKED = `
❯ QMULTI please
${RULE46}
←  ☒ Plan  ☒ Pages  ✔ Submit  →

Which pages should get the new banner?

  1. [ ] Home
         The first page people see
  2. [ ] Cart
         Where people pay
  3. [ ] Help
         Questions and answers
❯ 4. [✔] Footer row
     Submit
${RULE46}
  5. Chat about this

Enter to select · Tab/Arrow keys to navigate ·
ctrl+g to edit in Vim · Esc to cancel
`

// Then Tab: the cursor on the Submit row.
export const ON_SUBMIT = `
❯ QMULTI please
${RULE46}
←  ☒ Plan  ☒ Pages  ✔ Submit  →

Which pages should get the new banner?

  1. [ ] Home
         The first page people see
  2. [ ] Cart
         Where people pay
  3. [ ] Help
         Questions and answers
  4. [✔] Footer row
❯    Submit
${RULE46}
  5. Chat about this

Enter to select · Tab/Arrow keys to navigate ·
ctrl+g to edit in Vim · Esc to cancel
`

// Three questions in one call (then two more in a second call): the first, its cursor on the first option. Claude Code
// stays blocked through all three, its report the same; only the screen moves on.
export const THREE_Q1 = `
❯ QTHREE please
${RULE46}
←  ☐ Shelf  ☐ Color  ☐ Count  ✔ Submit  →

Which shelf should the fake lamp go on?

❯ 1. Top shelf
     Seen first
  2. Bottom shelf
     Near the floor
  3. Type something.
${RULE46}
  4. Chat about this

Enter to select · Tab/Arrow keys to navigate ·
Esc to cancel
`

// The same question, ↓ pressed: only the cursor moved.
export const THREE_Q1_DOWN = `
❯ QTHREE please
${RULE46}
←  ☐ Shelf  ☐ Color  ☐ Count  ✔ Submit  →

Which shelf should the fake lamp go on?

  1. Top shelf
     Seen first
❯ 2. Bottom shelf
     Near the floor
  3. Type something.
${RULE46}
  4. Chat about this

Enter to select · Tab/Arrow keys to navigate ·
Esc to cancel
`

// 1 picked: the second of the three.
export const THREE_Q2 = `
❯ QTHREE please
${RULE46}
←  ☒ Shelf  ☐ Color  ☐ Count  ✔ Submit  →

Which color should the fake lamp be?

❯ 1. Amber
     Warm light
  2. White
     Plain light
  3. Type something.
${RULE46}
  4. Chat about this

Enter to select · Tab/Arrow keys to navigate ·
Esc to cancel
`
