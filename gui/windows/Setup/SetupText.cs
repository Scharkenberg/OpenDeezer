// The installer's messages. They show before the app's resources can load (MRT is part of the
// Windows App SDK), so they live here in the app's seven languages, in Windows' display language.

using System;
using System.Globalization;
using System.Runtime.InteropServices;

namespace PurpleDisc;

internal static partial class SetupText
{
    private const string Caption = "Purple Disc for Deezer";
    private const uint MB_OK = 0x0, MB_YESNO = 0x4, MB_ICONWARNING = 0x30, MB_ICONQUESTION = 0x20, MB_DEFBUTTON2 = 0x100;
    private const uint MB_SETFOREGROUND = 0x10000, MB_RIGHT = 0x80000, MB_RTLREADING = 0x100000;
    private const int IDYES = 6;

    private static readonly string s_lang = CultureInfo.CurrentUICulture.TwoLetterISOLanguageName;

    // {0} the installed version, {1} this one | {0} the folder | {0} the reason | no placeholder.
    internal static readonly (string Older, string Busy, string Failed, string DeleteData) T = s_lang switch
    {
        "zh" => (
            "已安装 Purple Disc {0}，比此安装程序（{1}）更新。\n\n仍要替换为 {1} 吗？",
            "无法更新 Purple Disc，因为它的文件夹正在使用中：\n{0}\n\n请关闭 Purple Disc 以及打开了该文件夹的所有窗口，然后重新运行安装程序。",
            "无法安装 Purple Disc。\n\n{0}",
            "同时删除你的设置和下载内容吗？\n\n这会让你退出登录，并删除为离线收听保存的音乐。Purple Disc 终端应用使用相同的设置，也会失去这些设置。"),
        "hi" => (
            "Purple Disc {0} इंस्टॉल है, जो इस इंस्टॉलर ({1}) से नया है।\n\nक्या फिर भी इसे {1} से बदलें?",
            "Purple Disc अपडेट नहीं हो सका, क्योंकि इसका फ़ोल्डर उपयोग में है:\n{0}\n\nPurple Disc और उस फ़ोल्डर पर खुली हर विंडो बंद करें, फिर इंस्टॉलर दोबारा चलाएँ।",
            "Purple Disc इंस्टॉल नहीं हो सका।\n\n{0}",
            "क्या अपनी सेटिंग्स और डाउनलोड भी हटाएँ?\n\nइससे आप साइन आउट हो जाएँगे और ऑफ़लाइन सुनने के लिए रखा गया संगीत हट जाएगा। Purple Disc टर्मिनल ऐप भी यही सेटिंग्स इस्तेमाल करता है, इसलिए उसकी सेटिंग्स भी हट जाएँगी।"),
        "es" => (
            "Está instalado Purple Disc {0}, una versión más reciente que este instalador ({1}).\n\n¿Reemplazarla por la {1} de todos modos?",
            "No se pudo actualizar Purple Disc porque su carpeta está en uso:\n{0}\n\nCierra Purple Disc y cualquier ventana abierta en esa carpeta y vuelve a ejecutar el instalador.",
            "No se pudo instalar Purple Disc.\n\n{0}",
            "¿Eliminar también tu configuración y tus descargas?\n\nSe cerrará tu sesión y se eliminará la música guardada para escuchar sin conexión. La aplicación de terminal de Purple Disc usa la misma configuración y también la perderá."),
        "fr" => (
            "Purple Disc {0} est installé, une version plus récente que ce programme d’installation ({1}).\n\nLe remplacer quand même par la version {1} ?",
            "Impossible de mettre à jour Purple Disc, car son dossier est en cours d’utilisation :\n{0}\n\nFermez Purple Disc et toute fenêtre ouverte sur ce dossier, puis relancez le programme d’installation.",
            "Impossible d’installer Purple Disc.\n\n{0}",
            "Supprimer aussi vos paramètres et vos téléchargements ?\n\nVous serez déconnecté et la musique conservée pour l’écoute hors connexion sera supprimée. L’application Purple Disc pour le terminal utilise les mêmes paramètres et les perdra aussi."),
        "ar" => (
            "الإصدار {0} من Purple Disc مثبّت، وهو أحدث من برنامج التثبيت هذا ({1}).\n\nهل تريد استبداله بالإصدار {1} على أي حال؟",
            "تعذّر تحديث Purple Disc لأن مجلده قيد الاستخدام:\n{0}\n\nأغلق Purple Disc وأي نافذة مفتوحة على ذلك المجلد، ثم شغّل برنامج التثبيت مرة أخرى.",
            "تعذّر تثبيت Purple Disc.\n\n{0}",
            "هل تريد أيضًا حذف إعداداتك وتنزيلاتك؟\n\nسيؤدي ذلك إلى تسجيل خروجك وحذف الموسيقى المحفوظة للاستماع دون اتصال. يستخدم تطبيق Purple Disc للطرفية الإعدادات نفسها وسيفقدها أيضًا."),
        "ru" => (
            "Установлена Purple Disc {0} — более новая версия, чем этот установщик ({1}).\n\nВсё равно заменить её версией {1}?",
            "Не удалось обновить Purple Disc: её папка используется.\n{0}\n\nЗакройте Purple Disc и все окна, открытые в этой папке, и снова запустите установщик.",
            "Не удалось установить Purple Disc.\n\n{0}",
            "Удалить также ваши настройки и загрузки?\n\nВы выйдете из аккаунта, а музыка, сохранённая для прослушивания офлайн, будет удалена. Терминальное приложение Purple Disc использует те же настройки и тоже их потеряет."),
        _ => (
            "Purple Disc {0} is installed, which is newer than this installer ({1}).\n\nReplace it with {1} anyway?",
            "Purple Disc couldn't be updated because its folder is in use:\n{0}\n\nClose Purple Disc and any window open on that folder, then run the installer again.",
            "Purple Disc couldn't be installed.\n\n{0}",
            "Also delete your settings and downloads?\n\nThis signs you out and removes the music kept for offline listening. The Purple Disc terminal app uses the same settings and would lose them too."),
    };

    // The choice of apps: {0} this installer's version | the note | three command links (title\nnote).
    private static readonly (string Title, string Note, string Desktop, string Terminal, string Both) C = s_lang switch
    {
        "zh" => ("安装 Purple Disc {0}", "选择其中一个或两个都装。它们共用登录、设置和下载内容。",
            "桌面应用\n带独立窗口的完整应用。", "终端应用\n在命令行中使用的播放器。", "两者都装"),
        "hi" => ("Purple Disc {0} इंस्टॉल करें", "एक या दोनों चुनें। दोनों एक ही साइन-इन, सेटिंग्स और डाउनलोड इस्तेमाल करते हैं।",
            "डेस्कटॉप ऐप\nअपनी विंडो वाला पूरा ऐप।", "टर्मिनल ऐप\nकमांड लाइन के लिए प्लेयर।", "दोनों"),
        "es" => ("Instalar Purple Disc {0}", "Elige una o las dos. Comparten el inicio de sesión, la configuración y las descargas.",
            "Aplicación de escritorio\nLa aplicación completa, con su propia ventana.", "Aplicación de terminal\nUn reproductor para la línea de comandos.", "Las dos"),
        "fr" => ("Installer Purple Disc {0}", "Choisissez l’une ou les deux. Elles partagent la connexion, les paramètres et les téléchargements.",
            "Application de bureau\nL’application complète, dans sa propre fenêtre.", "Application pour le terminal\nUn lecteur pour la ligne de commande.", "Les deux"),
        "ar" => ("تثبيت Purple Disc {0}", "اختر واحدًا أو كليهما. يتشاركان تسجيل الدخول والإعدادات والتنزيلات.",
            "تطبيق سطح المكتب\nالتطبيق الكامل بنافذته الخاصة.", "تطبيق الطرفية\nمشغّل لسطر الأوامر.", "كلاهما"),
        "ru" => ("Установка Purple Disc {0}", "Выберите одно приложение или оба. У них общие вход, настройки и загрузки.",
            "Приложение для рабочего стола\nПолное приложение в собственном окне.", "Терминальное приложение\nПлеер для командной строки.", "Оба"),
        _ => ("Install Purple Disc {0}", "Pick one or both. They share your sign-in, settings and downloads.",
            "Desktop app\nThe full app, in its own window.", "Terminal app\nA player for the command line.", "Both"),
    };

    private static uint Rtl => s_lang == "ar" ? MB_RIGHT | MB_RTLREADING : 0;

    // Which apps to install, as Installer's flags (1 desktop, 2 terminal, 3 both), or 0 when the
    // dialog was cancelled. `preset` is the default button: what is installed now.
    internal static unsafe int Pick(string version, int preset)
    {
        fixed (char* title = Caption, main = string.Format(C.Title, version), note = C.Note, desktop = C.Desktop, terminal = C.Terminal, both = C.Both)
        {
            var buttons = stackalloc TASKDIALOG_BUTTON[] { new() { id = 1, text = desktop }, new() { id = 2, text = terminal }, new() { id = 3, text = both } };
            var config = new TASKDIALOGCONFIG
            {
                cbSize = (uint)sizeof(TASKDIALOGCONFIG),
                dwFlags = TDF_USE_HICON_MAIN | TDF_ALLOW_DIALOG_CANCELLATION | TDF_USE_COMMAND_LINKS | (s_lang == "ar" ? TDF_RTL_LAYOUT : 0),
                dwCommonButtons = TDCBF_CANCEL_BUTTON,
                pszWindowTitle = title,
                hMainIcon = NativeMethods.ExtractIconW(NativeMethods.GetModuleHandleW(null), Environment.ProcessPath!, 0),
                pszMainInstruction = main,
                pszContent = note,
                cButtons = 3,
                pButtons = buttons,
                nDefaultButton = preset,
            };
            int button;
            return TaskDialogIndirect(&config, &button, null, null) == 0 && button is >= 1 and <= 3 ? button : 0;
        }
    }

    private const uint TDF_USE_HICON_MAIN = 0x2, TDF_ALLOW_DIALOG_CANCELLATION = 0x8, TDF_USE_COMMAND_LINKS = 0x10, TDF_RTL_LAYOUT = 0x2000, TDCBF_CANCEL_BUTTON = 0x8;

    // commctrl.h packs these to 1; every field here is 4 or 8 bytes long, so the offsets are the natural ones.
    [StructLayout(LayoutKind.Sequential, Pack = 1)]
    private unsafe struct TASKDIALOG_BUTTON { public int id; public char* text; }

    [StructLayout(LayoutKind.Sequential, Pack = 1)]
    private unsafe struct TASKDIALOGCONFIG
    {
        public uint cbSize;
        public nint hwndParent, hInstance;
        public uint dwFlags, dwCommonButtons;
        public char* pszWindowTitle;
        public nint hMainIcon;
        public char* pszMainInstruction, pszContent;
        public uint cButtons;
        public TASKDIALOG_BUTTON* pButtons;
        public int nDefaultButton;
        public uint cRadioButtons;
        public nint pRadioButtons;
        public int nDefaultRadioButton;
        public nint pszVerificationText, pszExpandedInformation, pszExpandedControlText, pszCollapsedControlText, hFooterIcon, pszFooter, pfCallback, lpCallbackData;
        public uint cxWidth;
    }

    [LibraryImport("comctl32.dll")]
    private static unsafe partial int TaskDialogIndirect(TASKDIALOGCONFIG* config, int* button, int* radio, int* verification);

    // A yes/no question whose default is No.
    internal static bool Ask(string text) =>
        MessageBoxW(0, text, Caption, MB_YESNO | MB_ICONQUESTION | MB_DEFBUTTON2 | MB_SETFOREGROUND | Rtl) == IDYES;

    internal static void Tell(string text) => MessageBoxW(0, text, Caption, MB_OK | MB_ICONWARNING | MB_SETFOREGROUND | Rtl);

    [LibraryImport("user32.dll", StringMarshalling = StringMarshalling.Utf16)]
    private static partial int MessageBoxW(nint hWnd, string text, string caption, uint type);
}
