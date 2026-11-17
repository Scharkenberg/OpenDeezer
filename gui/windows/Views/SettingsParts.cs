// Settings rows with their own state: the output device, the folders and the stream cache.

using System;
using System.Collections.Generic;
using System.Threading.Tasks;
using CommunityToolkit.WinUI.Controls;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Windows.System;

namespace OpenDeezer;

internal sealed class DeviceSetting
{
    private readonly ComboBox _combo;
    private IReadOnlyList<AudioDevice> _devices = [];

    public SettingsCard Root { get; }

    public DeviceSetting()
    {
        _combo = Ui.Combo([], -1, i =>
        {
            string id = _devices[i].Id;
            App.Session.Settings.AudioDevice = id;
            Engine.Post(() => DeezerCore.DZSetAudioDevice(id));
            App.Session.SaveSettingsSoon();
        });
        _combo.MaxWidth = Size.FieldMax;
        Root = Ui.Setting("Settings_OutputDevice", null, Icons.Speakers, _combo);
    }

    public void Set(IReadOnlyList<AudioDevice> devices, string current)
    {
        _devices = devices;
        Ui.Quietly(_combo, () =>
        {
            _combo.Items.Clear();
            int selected = 0;
            for (int i = 0; i < devices.Count; i++)
            {
                string label = devices[i].Name.Length == 0 ? Loc.S("Settings_SystemDefault") : devices[i].Name;
                _combo.Items.Add(devices[i].IsDefault ? Loc.Format("Settings_DeviceDefaultFormat", label) : label);
                if (devices[i].Id == current) selected = i;
            }
            _combo.IsEnabled = devices.Count > 0;
            _combo.PlaceholderText = devices.Count > 0 ? "" : Loc.S("Settings_NoOutputDevices");
            _combo.SelectedIndex = devices.Count > 0 ? selected : -1;
        });
    }
}

// The Storage group's cards: while one of them waits for the engine all are disabled, so no
// change can start twice; a call that fails (or throws) toasts its reason, then raises Failed so
// every card shows the engine's values again.
internal sealed class StorageGroup
{
    private readonly List<Control> _cards = new();

    public event Action? Failed;

    public void Add(params Control[] cards) => _cards.AddRange(cards);

    // `call` runs off the UI thread and answers "" or the reason it failed. True when it worked.
    public async Task<bool> Run(string titleKey, Func<string> call)
    {
        foreach (var c in _cards) c.IsEnabled = false;
        string error;
        try { error = await Task.Run(call); }
        catch (Exception ex)
        {
            AppLog.Write($"storage: {ex.Message}");
            error = ex.Message;
        }
        finally
        {
            foreach (var c in _cards) c.IsEnabled = true;
        }
        if (error.Length == 0) return true;
        Toast.Error(Loc.S(titleKey), error);
        Failed?.Invoke();
        return false;
    }

    // A confirmation: its text, then one button per choice (the first is the accent one).
    public static Flyout Confirm(string textKey, params (string LabelKey, Action Chosen)[] choices)
    {
        var text = Ui.Text(Loc.S(textKey), wrap: true);
        var row = Ui.Row(Space.S);
        var flyout = Ui.Flyout(null, Ui.Stack(Space.M, text, row));
        for (int i = 0; i < choices.Length; i++)
        {
            var chosen = choices[i].Chosen;
            row.Children.Add(Ui.Button(Loc.S(choices[i].LabelKey), () =>
            {
                flyout.Hide();
                chosen();
            }, accent: i == 0));
        }
        return flyout;
    }
}

// A folder typed or picked: the download folder and the stream cache's folder. `apply` runs off
// the UI thread and answers "" or the reason the folder was refused (the box then goes back).
// With `confirmKey`, a change is confirmed first.
internal sealed class FolderSetting
{
    private readonly TextBox _box = new() { MinWidth = Size.FieldMin };
    private readonly string _headerKey;
    private readonly string? _confirmKey;
    private readonly Func<string, string> _apply;
    private readonly StorageGroup _group;
    private string _applied = "";
    private bool _asking;

    public SettingsCard Root { get; }

    public event Action? Applied;

    public FolderSetting(string headerKey, string glyph, StorageGroup group, Func<string, string> apply, string? confirmKey = null)
    {
        _headerKey = headerKey;
        _confirmKey = confirmKey;
        _apply = apply;
        _group = group;
        _box.LostFocus += (_, _) => Apply(_box.Text);
        _box.KeyDown += (_, e) =>
        {
            if (e.Key == VirtualKey.Enter) Apply(_box.Text);
        };
        Root = Ui.Setting(headerKey, null, glyph, Ui.Grid("* auto", Space.S, _box, Ui.Button(Loc.S("Btn_Browse"), Browse)));
        group.Add(Root);
    }

    public void Set(string dir)
    {
        _applied = dir;
        _box.Text = dir;
    }

    private async void Apply(string dir)
    {
        dir = dir.Trim();
        if (dir == _applied || _asking) return;
        if (_confirmKey != null && !await Confirmed())
        {
            Set(_applied);
            return;
        }
        string previous = _applied;
        _applied = dir;
        if (await _group.Run(_headerKey, () => _apply(dir))) Applied?.Invoke();
        else Set(previous);
    }

    private async Task<bool> Confirmed()
    {
        _asking = true;
        var answer = new TaskCompletionSource<bool>();
        var flyout = StorageGroup.Confirm(_confirmKey!, ("Btn_Change", () => answer.TrySetResult(true)), ("Btn_Cancel", () => answer.TrySetResult(false)));
        flyout.Closed += (_, _) => answer.TrySetResult(false);
        flyout.ShowAt(_box);
        bool ok = await answer.Task;
        _asking = false;
        return ok;
    }

    private async void Browse()
    {
        var picker = new Windows.Storage.Pickers.FolderPicker { SuggestedStartLocation = Windows.Storage.Pickers.PickerLocationId.Downloads };
        // PickSingleFolderAsync requires a file type filter.
        picker.FileTypeFilter.Add("*");
        WinRT.Interop.InitializeWithWindow.Initialize(picker, App.WindowHandle);
        Windows.Storage.StorageFolder? folder;
        try { folder = await picker.PickSingleFolderAsync(); }
        catch (Exception ex)
        {
            AppLog.Write("folder picker: " + ex.Message);
            return;
        }
        if (folder == null || folder.Path.Length == 0) return;
        _box.Text = folder.Path;
        Apply(folder.Path);
    }
}

// The stream cache's size and how long unplayed songs stay (both applied at once), what it
// holds, and clearing it. A value set elsewhere shows as it is in its list.
internal sealed class CacheSetting
{
    private readonly ComboBox _size, _age;
    private readonly List<int> _sizesMb = new(), _agesDays = new();
    private readonly StorageGroup _group;

    public SettingsCard SizeCard { get; }
    public SettingsCard AgeCard { get; }
    public SettingsCard ClearCard { get; }

    public CacheSetting(StorageGroup group)
    {
        _group = group;
        _size = Ui.Combo([], -1, i =>
        {
            int mb = _sizesMb[i];
            Apply("Settings_StreamCache", () => DeezerCore.SetMediaCacheMB(mb));
        });
        SizeCard = Ui.Setting("Settings_StreamCache", null, Icons.Disk, _size);
        _age = Ui.Combo([], -1, i =>
        {
            int days = _agesDays[i];
            Apply("Settings_CacheAge", () => DeezerCore.DZSetMediaCacheMaxAgeDays(days) == 1);
        });
        AgeCard = Ui.Setting("Settings_CacheAge", "Settings_CacheAgeDesc", Icons.Recent, _age);
        var clear = Ui.IconButton(Icons.ChevronRight, "Settings_ClearCache", null);
        clear.Flyout = StorageGroup.Confirm("Settings_ClearCacheConfirm",
            ("Btn_Clear", () => Apply("Settings_ClearCache", () => DeezerCore.DZClearMediaCache(0) == 1)),
            ("Settings_ClearCacheOffline", () => Apply("Settings_ClearCache", () => DeezerCore.DZClearMediaCache(1) == 1)));
        ClearCard = Ui.Setting("Settings_ClearCache", null, Icons.Delete, clear);
        group.Add(SizeCard, AgeCard, ClearCard);
    }

    public void Set(int mb, int maxAgeDays, MediaCacheStats stats)
    {
        Describe(stats);
        Fill(_size, _sizesMb, Array.ConvertAll(Steps.CacheSizesGb, gb => gb * 1024), mb, s => s == 0 ? Loc.S("Common_Off") : Fmt.Bytes(s * 1048576L));
        Fill(_age, _agesDays, Steps.CacheAgesDays, maxAgeDays,
            d => Array.IndexOf(Steps.CacheAgesDays, d) >= 0 ? Loc.S("Settings_CacheAge_" + d) : Loc.Plural("Days", d));
    }

    // The presets, with `value` inserted in order when it is not one of them, and selected.
    private static void Fill(ComboBox combo, List<int> values, int[] presets, int value, Func<int, string> label)
    {
        values.Clear();
        values.AddRange(presets);
        int at = values.FindIndex(v => v > value);
        if (!values.Contains(value)) values.Insert(at < 0 ? values.Count : at, value);
        Ui.Quietly(combo, () =>
        {
            combo.Items.Clear();
            foreach (int v in values) combo.Items.Add(label(v));
            combo.SelectedIndex = values.IndexOf(value);
        });
    }

    // After a change: what the cache holds now.
    public async void Refresh() => Describe(await Task.Run(DeezerCore.MediaCacheStats));

    private void Describe(MediaCacheStats stats) =>
        SizeCard.Description = stats.PinnedBytes > 0
            ? Loc.Format("Settings_CacheUsedOfflineFormat", Fmt.Bytes(stats.Bytes), Fmt.Bytes(stats.PinnedBytes))
            : Loc.Format("Settings_CacheUsedFormat", Fmt.Bytes(stats.Bytes));

    private async void Apply(string titleKey, Func<bool> call)
    {
        await _group.Run(titleKey, () => call() ? "" : Loc.S("Error_StorageFailed"));
        Refresh();
        // Clearing can take the offline music with it.
        await App.Offline.LoadAsync();
    }
}
