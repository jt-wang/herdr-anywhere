import XCTest
// How much is drawn in a strip of the screen (points), from a screenshot: the pixels that differ clearly from the
// strip's most common colour, its background. A terminal row with text has many; a blank one, or a terminal page not
// drawn yet, none. Used before a tap on text the test means to hit, which a page still connecting doesn't show yet.
func drawnPixels(in strip: CGRect, of app: XCUIApplication) -> Int {
  guard let cg = XCUIScreen.main.screenshot().image.cgImage, app.frame.width > 0 else { return 0 }
  let scale = CGFloat(cg.width) / app.frame.width
  let px = CGRect(x: strip.minX * scale, y: strip.minY * scale, width: strip.width * scale, height: strip.height * scale).integral
  guard let crop = cg.cropping(to: px) else { return 0 }
  let w = crop.width, h = crop.height
  var data = [UInt8](repeating: 0, count: w * h * 4)
  guard let ctx = CGContext(data: &data, width: w, height: h, bitsPerComponent: 8, bytesPerRow: w * 4,
                            space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else { return 0 }
  ctx.draw(crop, in: CGRect(x: 0, y: 0, width: w, height: h))
  var counts: [UInt32: Int] = [:]
  for i in stride(from: 0, to: data.count, by: 4) {
    let key = UInt32(data[i] >> 4) << 8 | UInt32(data[i + 1] >> 4) << 4 | UInt32(data[i + 2] >> 4)
    counts[key, default: 0] += 1
  }
  guard let bg = counts.max(by: { $0.value < $1.value })?.key else { return 0 }
  let br = Int(bg >> 8 & 15) * 16, bgr = Int(bg >> 4 & 15) * 16, bb = Int(bg & 15) * 16
  var n = 0
  for i in stride(from: 0, to: data.count, by: 4) where abs(Int(data[i]) - br) + abs(Int(data[i + 1]) - bgr) + abs(Int(data[i + 2]) - bb) > 120 { n += 1 }
  return n
}
