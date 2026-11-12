// The app's fonts, for the app and for every page that stands in for it (the website's pictures and the picture sheet),
// so they all measure text as the app does.
import '../assets/fonts/Geist.ttf'
// The new-tab sheet's title in Geist SemiBold, under its own family (scripts/title-font.py), so no other text changes.
// Every other heavier weight is Regular thickened by the system. On Android that title, thickened, came out wider once a
// terminal page had opened than the width NSH had measured for it, and broke after "Add an" onto a line it had no room for
// (e2e/android-sheet-title.sh).
import '../assets/fonts/GeistTitle-SemiBold.ttf'
import '../assets/fonts/JetBrainsMono-Variable.ttf'
