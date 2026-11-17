// The two modal dialogs: confirm and a one-line prompt.

using System;
using System.Threading;
using System.Threading.Tasks;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace OpenDeezer;

internal static class Dialogs
{
    // WinUI allows one open ContentDialog; later ones wait here instead of throwing.
    private static readonly SemaphoreSlim s_gate = new(1, 1);

    public static XamlRoot? Root { get; set; }

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

    // The entered text, trimmed; "" when cancelled.
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
