# Oh My Pi drawing pictures: what the phone page was given

Oh My Pi 18.18.3 draws pictures with the Kitty graphics protocol when it takes the terminal for one that shows them
(herdr-core sets `TERM_PROGRAM=ghostty` in its terminals). Each file holds the strings the phone page's xterm was given,
write by write, while omp drew one picture, on a 390×844 CSS px phone page (50 columns, 37 rows):

- `open`: the pane opened before omp drew (omp starts with `a=d,d=A`, delete all).
- `live`: the pane open while omp drew: the PNG in 4096-character pieces (`a=t,f=100,m=1` … `m=0`), its placement
  (`a=p,U=1,c=…,r=…`) and the placeholder cells (U+10EEEE, the picture's id in their colour, row and column in their
  marks).
- `reload`: a new connection's first attach, with herdr-core's history.
- `back`: the pane left and opened again in the same run: herdr-core sends one screen of placeholders, no picture data.

`omp-18.18.3-debug.json` is omp's own self-test picture (`/debug` → protocol, 192×128, 20 by 7 cells).
`omp-18.18.3-read.json` is a made-up 240×160 bar chart omp read with its `read` tool for a model and showed in the
tool's output (sent at 300×200, 48 by 16 cells).

How they were made: omp ran in a container with no network and no host folders, under herdr-core, in a made-up
project folder outside /Users and /home (`/work/sam/acme-app`, which omp shows in its status line and links), with
its first-run setup skipped (`OMP_SKIP_SETUP=1`). The model was a local fake that answers the first turn with a call
of `read` on the chart and the next with one line of text; the model entry says it takes images
(`input: [text, image]`), without which omp does not show the picture. The page was the app's web build in headless
Chromium. omp picks a new id for each picture it draws, so another recording has other ids.

omp also shows a random tip at start, and some tips name inference providers. Had one of those been recorded, its
words would have been replaced by made-up words of the same length outside the graphics commands. The tips in these
two recordings name none, and nothing was replaced.
