// Strings from Strings\<locale>\Resources.resw through MRT Core. The language is set once
// before any UI is built; a change needs a restart because built elements keep their text.

using System;
using System.Globalization;
using Microsoft.UI.Xaml;
using Microsoft.Windows.ApplicationModel.Resources;

namespace PurpleDisc;

internal static class Loc
{
    private static readonly ResourceManager _rm = new();
    private static ResourceContext _ctx = _rm.CreateResourceContext();

    private static string _base = "en";
    private static CultureInfo _culture = CultureInfo.InvariantCulture;
    private static bool _rtl;

    internal static readonly (string Tag, string Native)[] Languages =
    {
        ("",       ""),
        ("en-US",  "English"),
        ("zh-CN",  "简体中文"),
        ("hi-IN",  "हिन्दी"),
        ("es-ES",  "Español"),
        ("fr-FR",  "Français"),
        ("ar-SA",  "العربية"),
        ("ru-RU",  "Русский"),
    };

    internal static void SetLanguage(string tag)
    {
        _ctx = _rm.CreateResourceContext();
        lock (_cache) _cache.Clear();
        // Windows' language maps onto the nearest shipped one, else English, so plural and RTL rules match.
        string resolved = string.IsNullOrEmpty(tag) ? NearestShipped(OsLanguage()) : tag;
        _ctx.QualifierValues["Language"] = resolved;
        _base = BaseOf(resolved);
        _culture = SafeCulture(resolved);
        _rtl = _base is "ar" or "he" or "fa" or "ur";
    }

    private static string OsLanguage() => CultureInfo.CurrentUICulture.Name;

    private static string NearestShipped(string os)
    {
        foreach (var l in Languages)
            if (!string.IsNullOrEmpty(l.Tag) && string.Equals(l.Tag, os, StringComparison.OrdinalIgnoreCase)) return l.Tag;
        string b = BaseOf(os);
        foreach (var l in Languages)
            if (!string.IsNullOrEmpty(l.Tag) && BaseOf(l.Tag) == b) return l.Tag;
        return "en-US";
    }

    private static string BaseOf(string tag)
    {
        if (string.IsNullOrEmpty(tag)) return "en";
        int dash = tag.IndexOfAny(new[] { '-', '_' });
        return (dash > 0 ? tag[..dash] : tag).ToLowerInvariant();
    }

    private static CultureInfo SafeCulture(string tag)
    {
        try { return string.IsNullOrEmpty(tag) ? CultureInfo.CurrentCulture : new CultureInfo(tag); }
        catch (CultureNotFoundException ex)
        {
            AppLog.Write("culture " + tag + ": " + ex.Message);
            return CultureInfo.InvariantCulture;
        }
    }

    internal static bool IsRtl => _rtl;

    // The app language's culture, for dates and numbers.
    internal static CultureInfo Culture => _culture;

    // Fully qualified so the enum reference cannot bind to this same-named property.
    internal static Microsoft.UI.Xaml.FlowDirection FlowDirection =>
        _rtl ? Microsoft.UI.Xaml.FlowDirection.RightToLeft : Microsoft.UI.Xaml.FlowDirection.LeftToRight;

    // Each MRT lookup is a native call, so values are cached per language.
    private static readonly System.Collections.Generic.Dictionary<string, string> _cache = new();

    internal static string S(string key)
    {
        lock (_cache)
        {
            if (_cache.TryGetValue(key, out var hit)) return hit;
        }
        string v = key;
        try
        {
            var c = _rm.MainResourceMap.TryGetValue("Resources/" + key, _ctx);
            if (c != null) v = c.ValueAsString;
        }
        catch (Exception ex) { AppLog.Write("resource " + key + ": " + ex.Message); }
        lock (_cache) _cache[key] = v;
        return v;
    }

    internal static string Format(string key, params object[] args) => Fill(S(key), args);

    // Replaces {0}..{9} with the arguments in the language's culture. Anything else, such as a
    // translation with a broken or extra placeholder, stays as written instead of throwing.
    internal static string Fill(string fmt, params object[] args)
    {
        var sb = new System.Text.StringBuilder(fmt.Length + 16);
        for (int i = 0; i < fmt.Length; i++)
        {
            if (fmt[i] == '{' && i + 2 < fmt.Length && fmt[i + 2] == '}' && char.IsAsciiDigit(fmt[i + 1]) && fmt[i + 1] - '0' < args.Length)
            {
                sb.Append(Convert.ToString(args[fmt[i + 1] - '0'], _culture));
                i += 2;
            }
            else sb.Append(fmt[i]);
        }
        return sb.ToString();
    }

    // The counted noun for the language's CLDR category (baseKey_One, _Few, ...), else _Other.
    internal static string Plural(string baseKey, long n)
    {
        string cat = PluralCategory(_base, n);
        string fmt = S(baseKey + "_" + cat);
        if (fmt == baseKey + "_" + cat)
        {
            fmt = S(baseKey + "_Other");
            if (fmt == baseKey + "_Other") fmt = "{0}";
        }
        return Fill(fmt, n.ToString("N0", _culture));
    }

    // Returns only the categories the language uses, matching the forms in its .resw.
    private static string PluralCategory(string lang, long n)
    {
        long an = Math.Abs(n);
        switch (lang)
        {
            case "zh":
            case "ja":
            case "ko":
            case "th":
                return "Other"; // no plural distinction
            case "en":
            case "es":
                return an == 1 ? "One" : "Other";
            case "fr":
            case "hi":
                return (an == 0 || an == 1) ? "One" : "Other";
            case "ru":
            {
                long m10 = an % 10, m100 = an % 100;
                if (m10 == 1 && m100 != 11) return "One";
                if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return "Few";
                return "Many"; // 0, 5-9, 11-14 and everything else integral
            }
            case "ar":
            {
                if (an == 0) return "Zero";
                if (an == 1) return "One";
                if (an == 2) return "Two";
                long m100 = an % 100;
                if (m100 >= 3 && m100 <= 10) return "Few";
                if (m100 >= 11 && m100 <= 99) return "Many";
                return "Other"; // n%100 in {0,1,2} with n>=100
            }
            default:
                return an == 1 ? "One" : "Other";
        }
    }
}
