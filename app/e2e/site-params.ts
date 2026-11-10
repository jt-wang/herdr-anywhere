// What fixtures/fake.tsx reads from `host.params` in a test, set here because this app is opened from the launcher:
// the "site" scene, the line typed to Pi in the phone's language, and the theme the phone is in.
import { colorScheme, host, locale } from 'nsh'
import { langFor } from '../src/strings/index.ts'

// The line typed to Pi from the phone, as someone using the app in that language would write it.
export const TYPED: Record<string, string> = {
  en: 'Add a logout test', 'zh-Hans': '加一个退出登录的测试', 'zh-Hant': '加一個登出的測試', es: 'Añade un test de cierre de sesión',
  ja: 'ログアウトのテストを追加して', ko: '로그아웃 테스트 추가해 줘', ru: 'Добавь тест на выход',
}
const params = { scene: 'site', typed: TYPED[langFor(locale())], scheme: colorScheme(), prefs: JSON.stringify({ opened: 2 }) }
Object.defineProperty(host, 'params', { value: params, configurable: true })
