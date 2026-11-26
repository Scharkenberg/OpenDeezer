// The engine's 10-band equalizer, preamp and mono switch. Changes apply live; the engine
// keeps the state, so it is read again on every visit, where another client may have changed it.

using System;
using System.Text.Json.Nodes;
using System.Threading.Tasks;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace PurpleDisc;

internal sealed class EqualizerView : View
{
    private readonly Slider[] _bands = new Slider[Count.EqBands];
    private readonly TextBlock[] _labels = new TextBlock[Count.EqBands];
    private readonly ComboBox _preset;
    private readonly ToggleSwitch _enabled, _mono;
    private readonly Slider _preamp;
    private readonly Grid _bandGrid = Ui.Rows("auto auto");
    private readonly ScrollViewer _scroller;
    private EQState _eq = new();

    public override FrameworkElement Root => _scroller;
    public override ScrollViewer? Scroller => _scroller;

    public EqualizerView()
    {
        _enabled = Ui.Switch(false, "Settings_Equalizer", on =>
        {
            EnableParts(on);
            _ = Send(new JsonObject { ["enabled"] = on });
        });
        _mono = Ui.Switch(false, "EQ_MonoAudio", on => _ = Send(new JsonObject { ["mono"] = on }));
        _preset = Ui.Combo([], -1, OnPreset);
        Ui.Name(_preset, Loc.S("EQ_Preset"));
        var reset = Ui.Button(Loc.S("EQ_Reset"), () => ApplyPreset("flat"));
        var preampPump = new LatestValuePump<double>(db => Send(new JsonObject { ["preampDb"] = db }));
        _preamp = Ui.Slider(-Steps.EqMaxDb, Steps.EqMaxDb, 0, "EQ_Preamp", preampPump.Post);
        _preamp.StepFrequency = Steps.EqDb;
        _preamp.MinWidth = Size.FieldMin;
        _preamp.ThumbToolTipValueConverter = new DbConverter();
        var zeroDbLine = new Border { Style = Ui.Styles.Get("DividerLine"), VerticalAlignment = VerticalAlignment.Center };
        Grid.SetColumnSpan(zeroDbLine, Count.EqBands);
        _bandGrid.Children.Add(zeroDbLine);
        for (int i = 0; i < Count.EqBands; i++)
        {
            int band = i;
            var pump = new LatestValuePump<double>(g => Send(new JsonObject { ["band"] = new JsonObject { ["index"] = band, ["gainDb"] = g } }));
            _bands[i] = Ui.Slider(-Steps.EqMaxDb, Steps.EqMaxDb, 0, "Settings_Equalizer", g =>
            {
                Ui.Quietly(_preset, () => _preset.SelectedIndex = _preset.Items.Count - 1);
                pump.Post(g);
            });
            _bands[i].Orientation = Orientation.Vertical;
            _bands[i].Height = Size.EqBand;
            _bands[i].StepFrequency = Steps.EqDb;
            _bands[i].HorizontalAlignment = HorizontalAlignment.Center;
            _bands[i].ThumbToolTipValueConverter = new DbConverter();
            _labels[i] = Ui.Text("", TextKind.SecondaryCaption);
            _labels[i].HorizontalAlignment = HorizontalAlignment.Center;
            _labels[i].Margin = Pad.BandLabel;
            _bandGrid.ColumnDefinitions.Add(new ColumnDefinition());
            Grid.SetColumn(_bands[i], i);
            Grid.SetColumn(_labels[i], i);
            Grid.SetRow(_labels[i], 1);
            _bandGrid.Children.Add(_bands[i]);
            _bandGrid.Children.Add(_labels[i]);
        }
        var bandsCard = new Border { Style = Ui.Styles.Card, Child = _bandGrid };
        var page = Ui.Stack(Space.XS,
            Ui.Setting("Settings_Equalizer", "Settings_EqualizerDesc", Icons.Equalizer, _enabled),
            Ui.Setting("EQ_Preset", null, Icons.Preset, Ui.Row(Space.S, _preset, reset)),
            bandsCard,
            Ui.Setting("EQ_Preamp", null, Icons.Gain, _preamp),
            Ui.Setting("EQ_MonoAudio", null, Icons.Headphones, _mono));
        _scroller = Ui.Page(Size.ContentMax, Ui.PageTitle(Loc.S("Settings_Equalizer")), page);
    }

    public override async void Show(object? arg, bool isBack)
    {
        var ct = Ct;
        var eq = await Task.Run(DeezerCore.EQ);
        if (ct.IsCancellationRequested) return;
        Ui.Quietly(_preset, () =>
        {
            _preset.Items.Clear();
            foreach (var name in eq.Presets) _preset.Items.Add(Fmt.Preset(name));
            _preset.Items.Add(Loc.S("EQ_Custom"));
        });
        Ui.Quietly(_enabled, () => _enabled.IsOn = eq.Enabled);
        Ui.Quietly(_mono, () => _mono.IsOn = eq.Mono);
        Ui.Quietly(_preamp, () => _preamp.Value = eq.PreampDb);
        ShowBands(eq);
        EnableParts(eq.Enabled);
    }

    // The band gains and the preset, which reads "Custom" when it is none of the list.
    private void ShowBands(EQState eq)
    {
        _eq = eq;
        int preset = Array.IndexOf([.. eq.Presets], eq.Preset);
        Ui.Quietly(_preset, () => _preset.SelectedIndex = preset >= 0 ? preset : eq.Presets.Count);
        for (int i = 0; i < Count.EqBands; i++)
        {
            var s = _bands[i];
            Ui.Quietly(s, () => s.Value = i < eq.GainsDb.Count ? eq.GainsDb[i] : 0);
            string label = Fmt.Band(i < eq.Bands.Count ? eq.Bands[i] : 0);
            _labels[i].Text = label;
            Microsoft.UI.Xaml.Automation.AutomationProperties.SetName(s, label);
        }
    }

    private void EnableParts(bool on)
    {
        _preset.IsEnabled = on;
        _preamp.IsEnabled = on;
        foreach (var s in _bands) s.IsEnabled = on;
    }

    // The last entry, "Custom", is a state, not a choice.
    private void OnPreset(int i)
    {
        if (i < _eq.Presets.Count) ApplyPreset(_eq.Presets[i]);
    }

    private async void ApplyPreset(string name)
    {
        await Send(new JsonObject { ["preset"] = name });
        ShowBands(await Task.Run(DeezerCore.EQ));
    }

    // A change the engine refuses is said, not dropped.
    private static async Task Send(JsonObject change)
    {
        string payload = change.ToJsonString();
        if (await Task.Run(() => DeezerCore.DZSetEQJSON(payload)) == 0) Toast.Error(Loc.S("EQ_Refused"), "");
    }
}
