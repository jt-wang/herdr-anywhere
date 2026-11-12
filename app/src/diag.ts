// Diagnostics: a hook the e2e build sets (ios-e2e.tsx sends each to the Companion, whose log shows the path) and
// fixtures/fake.tsx records. The app itself does nothing with them. Fields are addresses and program keys, never text.
export type Diag = (what: string, fields: Record<string, string>) => void
let hook: Diag = () => {}
export const setDiag = (f: Diag) => { hook = f }
export const diag: Diag = (what, fields) => hook(what, fields)
