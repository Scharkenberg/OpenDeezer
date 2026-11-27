using System.Diagnostics;
using Xunit;

namespace PurpleDisc.Tests;

public class SignatureTests
{
    // The dotnet host running the tests carries Microsoft's embedded Authenticode signature;
    // the test assembly, built here, carries none.
    [Fact]
    public void SignerReadsTheSigningCertificate()
    {
        Assert.Contains("Microsoft", Signature.Signer(Process.GetCurrentProcess().MainModule!.FileName));
        Assert.Null(Signature.Signer(typeof(SignatureTests).Assembly.Location));
    }
}
