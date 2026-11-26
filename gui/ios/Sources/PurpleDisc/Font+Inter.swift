import SwiftUI

extension Font {
    /// Inter at a text style's size, growing with Dynamic Type as the style does; the headline
    /// keeps its semibold.
    static func inter(_ style: Font.TextStyle) -> Font {
        let f = Font.custom("Inter Variable", size: interSize(style), relativeTo: style)
        return style == .headline ? f.weight(.semibold) : f
    }

    /// Inter at a fixed size.
    static func inter(size: CGFloat, weight: Font.Weight = .regular) -> Font {
        .custom("Inter Variable", size: size).weight(weight)
    }

    // The text styles' sizes at the default Dynamic Type setting (Large).
    private static func interSize(_ style: Font.TextStyle) -> CGFloat {
        switch style {
        case .largeTitle: 34
        case .title: 28
        case .title2: 22
        case .title3: 20
        case .headline, .body: 17
        case .callout: 16
        case .subheadline: 15
        case .footnote: 13
        case .caption: 12
        case .caption2: 11
        @unknown default: 17
        }
    }
}
