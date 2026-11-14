# What android-zoom.sh and ios-zoom.sh check on one run's screenshots (sourced): zoom_checks DIR SCHEME "BOX" BG,
# with DIR/SCHEME-<step>.png from fitted, pinched, corner, corner-again, out, doubletap, out2; BOX the picture's box
# in their pixels; BG the sheet's colour. Calls say "ok: …" or say "FAIL: …" for each.
zoom_checks() {
  d=$1 s=$2 box=$3 bg=$4
  zl() { python3 "$(dirname "$0")/zoom-look.py" "$@"; }
  big() { [ "${1%.*}" -ge 8 ]; }   # mean |Δ| of 8 or more: another part of the picture, or another size
  same() { [ "${1%.*}" -le 2 ]; }  # 2 or less: the same picture, give or take the encoder
  v=$(zl diff "$d/$s-fitted.png" "$d/$s-pinched.png" "$box"); big "$v" && say "ok: a pinch zooms the picture in (Δ $v)" || say "FAIL: a pinch left the picture as it was (Δ $v)"
  v=$(zl diff "$d/$s-pinched.png" "$d/$s-corner.png" "$box"); big "$v" && say "ok: a drag moves the zoomed picture (Δ $v)" || say "FAIL: a drag didn't move it (Δ $v)"
  v=$(zl diff "$d/$s-corner.png" "$d/$s-corner-again.png" "$box"); same "$v" && say "ok: dragged past the corner, it stays at the corner (Δ $v)" || say "FAIL: it moved on past the corner (Δ $v)"
  v=$(zl edge "$d/$s-corner.png" "$box" "$bg"); [ "${v%.*}" -le 3 ] && say "ok: no sheet shows past the picture's edge ($v %)" || say "FAIL: the sheet shows past the picture's edge ($v %)"
  v=$(zl diff "$d/$s-fitted.png" "$d/$s-out.png" "$box"); same "$v" && say "ok: a double tap zooms back out to the fitted picture (Δ $v)" || say "FAIL: a double tap didn't zoom out (Δ $v)"
  v=$(zl diff "$d/$s-fitted.png" "$d/$s-doubletap.png" "$box"); big "$v" && say "ok: a double tap zooms in (Δ $v)" || say "FAIL: a double tap didn't zoom in (Δ $v)"
  v=$(zl diff "$d/$s-fitted.png" "$d/$s-out2.png" "$box"); same "$v" && say "ok: and another back out (Δ $v)" || say "FAIL: the second double tap didn't zoom out (Δ $v)"
}
