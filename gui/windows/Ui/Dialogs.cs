// The modal dialogs: confirm, a form, a message and a one-line prompt.

using System;
using System.Threading;
using System.Threading.Tasks;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace PurpleDisc;

internal static class Dialogs
{
    // WinUI allows one open ContentDialog; later ones wait here instead of throwing.
    private static readonly SemaphoreSlim s_gate = new(1, 1);

    public static XamlRoot? Root { get; set; }

    // The theme settings.json asks for ("" follows Windows), on the window's content.
    public static void ApplyTheme()
    {
        if (Root?.Content is FrameworkElement root)
            root.RequestedTheme = App.Settings.Theme switch { "light" => ElementTheme.Light, "dark" => ElementTheme.Dark, _ => ElementTheme.Default };
    }

    public static async Task<bool> Confirm(string title, string body, string okText)
    {
        var dlg = new ContentDialog
        {
            Title = title,
            Content = Ui.Text(body, TextKind.Body, wrap: true),
            PrimaryButtonText = okText,
            CloseButtonText = Loc.S("Btn_Cancel"),
            DefaultButton = ContentDialogButton.Close,
        };
        return await Show(dlg) == ContentDialogResult.Primary;
    }

    // A form: true when its OK was chosen.
    public static async Task<bool> Form(string title, UIElement content, string okText) => await Show(new ContentDialog
    {
        Title = title,
        Content = content,
        PrimaryButtonText = okText,
        CloseButtonText = Loc.S("Btn_Cancel"),
        DefaultButton = ContentDialogButton.Primary,
    }) == ContentDialogResult.Primary;

    // A text to read, with Close.
    public static async Task Message(string title, string body)
    {
        var dlg = new ContentDialog
        {
            Title = title,
            Content = new ScrollViewer { Content = Ui.Text(body, TextKind.Body, wrap: true) },
            CloseButtonText = Loc.S("Btn_Close"),
            DefaultButton = ContentDialogButton.Close,
        };
        await Show(dlg);
    }

    // The entered text, trimmed; "" when canceled.
    public static async Task<string> Prompt(string title, string placeholder, string initial)
    {
        var box = new TextBox { PlaceholderText = placeholder, Text = initial };
        var dlg = new ContentDialog
        {
            Title = title,
            Content = box,
            PrimaryButtonText = Loc.S("Btn_OK"),
            CloseButtonText = Loc.S("Btn_Cancel"),
            DefaultButton = ContentDialogButton.Primary,
        };
        return await Show(dlg) == ContentDialogResult.Primary ? box.Text.Trim() : "";
    }

    private static async Task<ContentDialogResult> Show(ContentDialog dlg)
    {
        dlg.XamlRoot = Root;
        dlg.FlowDirection = Loc.FlowDirection;
        await s_gate.WaitAsync();
        try { return await dlg.ShowAsync(); }
        finally { s_gate.Release(); }
    }
}
