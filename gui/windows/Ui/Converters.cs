// Slider thumb tooltips: milliseconds as m:ss, and decibels as "+3 dB".

using System;
using Microsoft.UI.Xaml.Data;

namespace PurpleDisc;

internal sealed partial class TimeTextConverter : IValueConverter
{
    public object Convert(object value, Type targetType, object parameter, string language) => value is double d ? Fmt.Time((long)d) : "";

    public object ConvertBack(object value, Type targetType, object parameter, string language) => throw new NotSupportedException();
}

internal sealed partial class DbConverter : IValueConverter
{
    public object Convert(object value, Type targetType, object parameter, string language) =>
        value is double d ? (d > 0 ? "+" : "") + d.ToString("0.#", Loc.Culture) + " dB" : "";

    public object ConvertBack(object value, Type targetType, object parameter, string language) => throw new NotSupportedException();
}
