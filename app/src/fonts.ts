// The app's fonts, for the app and for every page that stands in for it (the website's pictures and the picture sheet),
// so they all measure text as the app does.
import '../assets/fonts/Geist.ttf'
// Shared DSH weights and mono face, bundled locally with their OFL (OFL-GeistMono.txt).
import '../assets/fonts/Geist-Medium.ttf'
import '../assets/fonts/Geist-SemiBold.ttf'
import '../assets/fonts/GeistMono-Regular.ttf'
// The new-tab sheet's title in Geist SemiBold, under its own family (scripts/title-font.py), so no other text changes.
// Keep that title's explicit family: on Android the old synthetic semibold came out wider once a
// terminal page had opened than the width NSH had measured for it, and broke after "Add an" onto a line it had no room for
// (e2e/android-sheet-title.sh).
import '../assets/fonts/GeistTitle-SemiBold.ttf'
import '../assets/fonts/JetBrainsMono-Variable.ttf'
