// A shortcut file through the shell's ShellLink object (IShellLinkW, IPersistFile), with
// source-generated COM so it works under Native AOT.

using System;
using System.IO;
using System.Runtime.InteropServices;
using System.Runtime.InteropServices.Marshalling;

namespace PurpleDisc;

internal static partial class ShellLink
{
    private static readonly Guid CLSID_ShellLink = new("00021401-0000-0000-C000-000000000046");
    private static readonly Guid IID_IShellLinkW = new("000214F9-0000-0000-C000-000000000046");
    private const uint CLSCTX_INPROC_SERVER = 1;

    internal static void Save(string path, string target, string description)
    {
        Marshal.ThrowExceptionForHR(CoCreateInstance(CLSID_ShellLink, 0, CLSCTX_INPROC_SERVER, IID_IShellLinkW, out nint unknown));
        var link = (IShellLinkW)new StrategyBasedComWrappers().GetOrCreateObjectForComInstance(unknown, CreateObjectFlags.None);
        Marshal.Release(unknown);
        link.SetPath(target);
        link.SetWorkingDirectory(Path.GetDirectoryName(target)!);
        link.SetDescription(description);
        Directory.CreateDirectory(Path.GetDirectoryName(path)!);
        ((IPersistFile)link).Save(path, true);
    }

    [LibraryImport("ole32.dll")]
    private static partial int CoCreateInstance(in Guid clsid, nint outer, uint context, in Guid iid, out nint unknown);
}

// The vtable order of shobjidl_core.h; only the setters are called.
[GeneratedComInterface(StringMarshalling = StringMarshalling.Utf16), Guid("000214F9-0000-0000-C000-000000000046")]
internal partial interface IShellLinkW
{
    void GetPath(nint file, int size, nint findData, uint flags);
    void GetIDList(out nint idList);
    void SetIDList(nint idList);
    void GetDescription(nint name, int size);
    void SetDescription(string name);
    void GetWorkingDirectory(nint dir, int size);
    void SetWorkingDirectory(string dir);
    void GetArguments(nint args, int size);
    void SetArguments(string args);
    void GetHotkey(out ushort hotkey);
    void SetHotkey(ushort hotkey);
    void GetShowCmd(out int showCmd);
    void SetShowCmd(int showCmd);
    void GetIconLocation(nint path, int size, out int index);
    void SetIconLocation(string path, int index);
    void SetRelativePath(string path, uint reserved);
    void Resolve(nint hwnd, uint flags);
    void SetPath(string file);
}

// IPersist's GetClassID first, then IPersistFile's own methods.
[GeneratedComInterface(StringMarshalling = StringMarshalling.Utf16), Guid("0000010b-0000-0000-C000-000000000046")]
internal partial interface IPersistFile
{
    void GetClassID(out Guid classId);
    [PreserveSig] int IsDirty();
    void Load(string file, uint mode);
    void Save(string file, [MarshalAs(UnmanagedType.Bool)] bool remember);
    void SaveCompleted(string file);
    void GetCurFile(out nint file);
}
