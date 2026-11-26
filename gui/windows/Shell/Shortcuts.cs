// Window-wide keys: Ctrl+F/E search, Alt+Left and XButton1 back, Ctrl+arrows for
// the transport and volume, Ctrl+L lyrics, Space play/pause.

using System;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Input;
using Windows.System;

namespace PurpleDisc;

internal static class Shortcuts
{
    private static Player Player => App.Player;

    public static void Attach(Grid root, Navigator nav, Func<bool> focusSearch)
    {
        bool TextInputFocused() =>
            FocusManager.GetFocusedElement(root.XamlRoot) is TextBox or PasswordBox or AutoSuggestBox or RichEditBox or NumberBox or WebView2;
        void Key(VirtualKey key, VirtualKeyModifiers mods, Func<bool> act)
        {
            var a = new KeyboardAccelerator { Key = key, Modifiers = mods };
            // The web sign-in page keeps the keyboard.
            a.Invoked += (_, args) => args.Handled = !nav.IsCurrent<WebSignInView>() && act();
            root.KeyboardAccelerators.Add(a);
        }
        bool Transport(Action act)
        {
            if (!App.Session.SignedIn || TextInputFocused()) return false;
            act();
            return true;
        }
        root.KeyboardAcceleratorPlacementMode = KeyboardAcceleratorPlacementMode.Hidden;
        Key(VirtualKey.F, VirtualKeyModifiers.Control, focusSearch);
        Key(VirtualKey.E, VirtualKeyModifiers.Control, focusSearch);
        Key(VirtualKey.Left, VirtualKeyModifiers.Menu, nav.Back);
        Key(VirtualKey.Right, VirtualKeyModifiers.Control, () => Transport(Player.Next));
        Key(VirtualKey.Left, VirtualKeyModifiers.Control, () => Transport(Player.Prev));
        Key(VirtualKey.Up, VirtualKeyModifiers.Control, () => Transport(() => Player.SetVolume(Math.Min(1, Player.Volume + Steps.Volume / 100))));
        Key(VirtualKey.Down, VirtualKeyModifiers.Control, () => Transport(() => Player.SetVolume(Math.Max(0, Player.Volume - Steps.Volume / 100))));
        Key(VirtualKey.L, VirtualKeyModifiers.Control, () => Transport(() =>
        {
            if (!nav.IsCurrent<LyricsView>()) nav.Push<LyricsView>();
        }));
        // Buttons and list items handle Space first; text inputs do not mark it handled.
        root.KeyDown += (_, e) =>
        {
            if (e.Key != VirtualKey.Space || e.Handled || !App.Session.SignedIn || TextInputFocused()) return;
            e.Handled = true;
            Player.TogglePlayPause();
        };
        root.AddHandler(UIElement.PointerPressedEvent, new PointerEventHandler((_, e) =>
        {
            if (e.GetCurrentPoint(root).Properties.IsXButton1Pressed) e.Handled = nav.Back();
        }), true);
    }
}
