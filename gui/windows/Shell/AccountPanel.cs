// Who is signed in: name, plan, Switch account and Sign out. Shown in the
// title-bar account flyout and as the first card in Settings.

using System;
using System.Threading.Tasks;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace OpenDeezer;

internal static class AccountPanel
{
    private static Session Session => App.Session;

    public static string Name => Session.Account.Name.Length > 0 ? Session.Account.Name : Loc.S("Account_Title");

    public static string Plan =>
        !Session.AccountKnown ? Loc.S("Account_UnknownTier")
        : Session.IsFree ? Loc.S("Account_FreePlan")
        : Session.Account.Offer;

    // Switch account and Sign out, side by side and equally wide; a long label wraps.
    public static Grid Actions(Action done)
    {
        Button Action(string key, Action run)
        {
            var b = Ui.Button(Loc.S(key), () =>
            {
                done();
                run();
            });
            var label = Ui.Text(Loc.S(key), wrap: true);
            label.TextAlignment = TextAlignment.Center;
            b.Content = label;
            b.HorizontalAlignment = HorizontalAlignment.Stretch;
            b.VerticalAlignment = VerticalAlignment.Stretch;
            return b;
        }
        return Ui.Grid("* *", Space.S, Action("Account_Switch", SwitchAccount), Action("Account_SignOut", SignOut));
    }

    // "Manage my plan": the plans page of deezer.com, in the app's language where Deezer has one.
    public static HyperlinkButton ManagePlan()
    {
        return Ui.Link(Loc.S("Account_ManagePlan"), async () =>
        {
            string lang = Loc.Culture.TwoLetterISOLanguageName is "fr" or "es" or "ru" or "ar" ? Loc.Culture.TwoLetterISOLanguageName : "en";
            await Windows.System.Launcher.LaunchUriAsync(new Uri($"https://www.deezer.com/{lang}/offers"));
        });
    }

    public static void SwitchAccount()
    {
        if (Session.Busy) return;
        App.Navigator.Push<WebSignInView>(new Func<string, Task>(arl => Session.SignInAsync(arl)));
    }

    public static async void SignOut()
    {
        if (await Dialogs.Confirm(Loc.S("Dialog_SignOutTitle"), Loc.S("Account_SignOutConfirm"), Loc.S("Dialog_SignOutTitle"))) Session.SignOut();
    }

    public static Button TitleBarButton()
    {
        var picture = Ui.Square(new PersonPicture(), Size.Avatar);
        // The flyout: who, the account actions, then quick playback settings (the ones the player
        // bar does not have), which follow the Settings page. Name and plan refresh on each opening.
        var name = Ui.Text("", TextKind.Strong);
        var plan = Ui.Text("", TextKind.SecondaryCaption, wrap: true);
        var quality = QuickSettings.Quality();
        var crossfade = QuickSettings.Crossfade();
        var replayGain = QuickSettings.ReplayGain();
        quality.Header = Loc.S("Settings_AudioQuality");
        crossfade.Header = Loc.S("Settings_Crossfade");
        replayGain.Header = Loc.S("Settings_VolumeNormalization");
        foreach (var c in new[] { quality, crossfade })
        {
            c.MinWidth = 0;
            c.HorizontalAlignment = HorizontalAlignment.Stretch;
        }
        Flyout? flyout = null;
        // A long name or plan wraps; the flyout keeps its width.
        var panel = new Border
        {
            Child = Ui.Stack(Space.S, Ui.Stack(0, name, plan, ManagePlan()), Actions(() => flyout!.Hide()), new Border { Style = Ui.Styles.Get("DividerLine") },
                quality, crossfade, replayGain),
        };
        flyout = Ui.Flyout(null, panel);
        flyout.Opening += (_, _) =>
        {
            name.Text = Name;
            plan.Text = Plan;
        };
        var button = Ui.Square(new Button { Style = Ui.Styles.IconButton, Content = picture, Flyout = flyout }, Size.IconButton);
        Ui.Name(button, Loc.S("Account_Title"));
        Session.AccountChanged += () =>
        {
            picture.DisplayName = Session.Account.Name;
            // No picture: the initials show.
            var image = Ui.Bitmap(Session.Account.Picture, Size.Avatar);
            if (image != null) image.ImageFailed += (_, _) => picture.ProfilePicture = null;
            picture.ProfilePicture = image;
            Ui.Name(button, Name);
        };
        button.VerticalAlignment = VerticalAlignment.Center;
        return button;
    }
}
