// The playback preferences shown both on the Settings page and in the account flyout. Each
// control follows PlaybackOptions while it is on screen, so a change in one place shows in the other.

using System;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace OpenDeezer;

internal static class QuickSettings
{
    public static ComboBox Quality()
    {
        var c = Ui.Combo([], -1, PlaybackOptions.SetQuality);
        foreach (var key in new[] { "Quality_Normal", "Quality_High", "Quality_HiFi" }) c.Items.Add(new ComboBoxItem { Content = Loc.S(key) });
        return Follow(c, () =>
        {
            // Tiers the plan cannot play are disabled.
            for (int i = 0; i < c.Items.Count; i++) ((ComboBoxItem)c.Items[i]).IsEnabled = PlaybackOptions.Allowed(i);
            Ui.Quietly(c, () => c.SelectedIndex = PlaybackOptions.Quality);
        });
    }

    public static ComboBox Crossfade()
    {
        var c = Ui.Combo(PlaybackOptions.CrossfadeNames, PlaybackOptions.CrossfadeIndex, PlaybackOptions.SetCrossfade);
        return Follow(c, () => Ui.Quietly(c, () => c.SelectedIndex = PlaybackOptions.CrossfadeIndex));
    }

    public static ToggleSwitch ReplayGain()
    {
        var sw = Ui.Switch(PlaybackOptions.ReplayGain, "Settings_VolumeNormalization", PlaybackOptions.SetReplayGain);
        return Follow(sw, () => Ui.Quietly(sw, () => sw.IsOn = PlaybackOptions.ReplayGain));
    }

    public static ToggleSwitch Gapless()
    {
        var sw = Ui.Switch(PlaybackOptions.Gapless, "Settings_GaplessPlayback", PlaybackOptions.SetGapless);
        return Follow(sw, () => Ui.Quietly(sw, () => sw.IsOn = PlaybackOptions.Gapless));
    }

    // Syncs `e` now and on every change while it is loaded.
    public static T Follow<T>(T e, Action sync) where T : FrameworkElement
    {
        Action handler = sync;
        e.Loaded += (_, _) =>
        {
            sync();
            PlaybackOptions.Changed -= handler;
            PlaybackOptions.Changed += handler;
        };
        e.Unloaded += (_, _) => PlaybackOptions.Changed -= handler;
        sync();
        return e;
    }
}
