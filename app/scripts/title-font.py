# Makes assets/fonts/GeistTitle-SemiBold.ttf: Geist-SemiBold.ttf of the geist-font v1.7.2 release with its family
# renamed "Geist Title" (name IDs 1, 4, 6 and 16), the rest unchanged. The app registers a face under its family, so
# under "Geist" it would be every font-semibold's; under its own family only the Text that names it uses it (src/fonts.ts).
# Needs fontTools. Usage: python3 scripts/title-font.py Geist-SemiBold.ttf assets/fonts/GeistTitle-SemiBold.ttf
import sys
from fontTools.ttLib import TTFont
f = TTFont(sys.argv[1], recalcTimestamp=False)  # the same bytes on each run
names = {1: 'Geist Title', 4: 'Geist Title SemiBold', 6: 'GeistTitle-SemiBold', 16: 'Geist Title'}
for r in f['name'].names:
    if r.nameID in names:
        r.string = names[r.nameID]
f.save(sys.argv[2])
