import SwiftUI

extension Font {
    /// Inter at a text style's size; the headline keeps its bold.
    static func inter(_ style: Font.TextStyle) -> Font {
        let f = Font.custom("Inter Variable", size: interSize(style), relativeTo: style)
        return style == .headline ? f.weight(.bold) : f
    }

    /// Inter at a fixed size.
    static func inter(size: CGFloat, weight: Font.Weight = .regular) -> Font {
        .custom("Inter Variable", size: size).weight(weight)
    }

    // The text styles' sizes on macOS.
    private static func interSize(_ style: Font.TextStyle) -> CGFloat {
        switch style {
        case .largeTitle: 26
        case .title: 22
        case .title2: 17
        case .title3: 15
        case .headline, .body: 13
        case .callout: 12
        case .subheadline: 11
        case .footnote, .caption, .caption2: 10
        @unknown default: 13
        }
    }
}
