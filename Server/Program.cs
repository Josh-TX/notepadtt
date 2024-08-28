using System.Text.RegularExpressions;

var builder = WebApplication.CreateBuilder(args);
builder.Services.AddControllers();
builder.Services.AddSignalR();
var app = builder.Build();
app.UseDefaultFiles();
app.UseStaticFiles();
app.MapControllers();
app.MapHub<MyHub>("main-hub");
app.UseDeveloperExceptionPage();

var title = Environment.GetEnvironmentVariable("title");
if (title != null)
{
    var indexPath = Path.Combine(Directory.GetCurrentDirectory(), "wwwroot", "index.html");
    if (File.Exists(indexPath))
    {
        string html = File.ReadAllText(indexPath);
        var updatedHtml = Regex.Replace(html, @"<title>(.*?)<\/title>", $"<title>{title}</title>", RegexOptions.IgnoreCase);
        File.WriteAllText(indexPath, updatedHtml);
    }
    else
    {
        Console.WriteLine("Title environment variable specified, but an error occurred updating the title");
    }
}

app.Run();