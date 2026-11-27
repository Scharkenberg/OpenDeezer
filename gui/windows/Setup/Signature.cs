// Who signed a file, when Windows trusts its Authenticode signature (WinVerifyTrust).

using System;
using System.Runtime.InteropServices;

namespace PurpleDisc;

internal static unsafe partial class Signature
{
    private static readonly Guid WINTRUST_ACTION_GENERIC_VERIFY_V2 = new("00AAC56B-CD44-11d0-8CC2-00C04FC295EE");
    private const uint WTD_UI_NONE = 2, WTD_REVOKE_NONE = 0, WTD_CHOICE_FILE = 1, WTD_STATEACTION_VERIFY = 1, WTD_STATEACTION_CLOSE = 2;
    private const uint CERT_NAME_RDN_TYPE = 2, CERT_X500_NAME_STR = 3;

    // The signer's subject, or null when the file is unsigned or its signature is not trusted.
    internal static string? Signer(string path)
    {
        fixed (char* p = path)
        {
            var file = new WINTRUST_FILE_INFO { cbStruct = (uint)sizeof(WINTRUST_FILE_INFO), pcwszFilePath = p };
            var data = new WINTRUST_DATA
            {
                cbStruct = (uint)sizeof(WINTRUST_DATA),
                dwUIChoice = WTD_UI_NONE,
                fdwRevocationChecks = WTD_REVOKE_NONE,
                dwUnionChoice = WTD_CHOICE_FILE,
                pFile = &file,
                dwStateAction = WTD_STATEACTION_VERIFY,
            };
            Guid action = WINTRUST_ACTION_GENERIC_VERIFY_V2;
            try
            {
                if (WinVerifyTrust(0, &action, &data) != 0) return null;
                // CRYPT_PROVIDER_SGNR.pasCertChain (after cbStruct, a FILETIME and csCertChain), then its first
                // CRYPT_PROVIDER_CERT's pCert: the signing certificate.
                nint signer = WTHelperGetProvSignerFromChain(WTHelperProvDataFromStateData(data.hWVTStateData), 0, 0, 0);
                nint cert = *(nint*)(*(nint*)(signer + 16) + 8);
                uint type = CERT_X500_NAME_STR;
                char* name = stackalloc char[512];
                return CertGetNameStringW(cert, CERT_NAME_RDN_TYPE, 0, &type, name, 512) > 1 ? new string(name) : null;
            }
            finally
            {
                data.dwStateAction = WTD_STATEACTION_CLOSE;
                WinVerifyTrust(0, &action, &data);
            }
        }
    }

    private struct WINTRUST_FILE_INFO
    {
        public uint cbStruct;
        public char* pcwszFilePath;
        public nint hFile;
        public Guid* pgKnownSubject;
    }

    private struct WINTRUST_DATA
    {
        public uint cbStruct;
        public nint pPolicyCallbackData, pSIPClientData;
        public uint dwUIChoice, fdwRevocationChecks, dwUnionChoice;
        public WINTRUST_FILE_INFO* pFile;
        public uint dwStateAction;
        public nint hWVTStateData, pwszURLReference;
        public uint dwProvFlags, dwUIContext;
        public nint pSignatureSettings;
    }

    [LibraryImport("wintrust.dll")]
    private static partial int WinVerifyTrust(nint hwnd, Guid* action, WINTRUST_DATA* data);

    [LibraryImport("wintrust.dll")]
    private static partial nint WTHelperProvDataFromStateData(nint stateData);

    [LibraryImport("wintrust.dll")]
    private static partial nint WTHelperGetProvSignerFromChain(nint provData, uint signer, int counterSigner, uint counterSignerIndex);

    [LibraryImport("crypt32.dll")]
    private static partial uint CertGetNameStringW(nint cert, uint type, uint flags, void* typePara, char* name, uint size);
}
