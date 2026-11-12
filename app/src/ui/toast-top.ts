/** Where the toast's top goes, in points from the top of the screen, given the safe area's top. On iOS it is where
 *  the system draws its own notification banner, 3 pt above the safe area on a phone with a notch or the Dynamic
 *  Island and 8 pt from the top, over the status bar, on one without (measured: src/ui/toast-top.test.ts). Android
 *  and the web keep it 12 pt below the status bar. */
export const toastTop = (os: string, safeTop: number) => os !== 'ios' ? safeTop + 12 : safeTop > 24 ? safeTop - 3 : 8
