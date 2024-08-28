using System.Security.Cryptography;
using System.Text.Json;
using System.Text.RegularExpressions;

public class StorageManager
{
    private const string _fileBasePath = "/data/";
    private const string _fileOrderName = ".notepad_file_order.txt";
    private static Info? _info;

    public TabContent LoadTabContent(Guid fileId)
    {
        var info = GetInfo();
        var tabInfo = info.TabInfos.FirstOrDefault(z => z.FileId == fileId);
        if (tabInfo == null){
            return new TabContent
            {
                FileId = fileId,
                Text = "\n\n\n\n"
            };
        }
        var fullPath = _fileBasePath + tabInfo.Filename;
        if (!System.IO.File.Exists(fullPath))
        {
            return new TabContent
            {
                FileId = fileId,
                Text = "\n\n\n\n"
            };
        }
        var text = System.IO.File.ReadAllText(fullPath);
        return new TabContent
        {
            FileId = fileId,
            Text = text
        };
    }

    public void SaveTabContent(TabContent tabContent)
    {
        var info = GetInfo();
        var tabInfo = info.TabInfos.FirstOrDefault(z => z.FileId == tabContent.FileId);
        if (tabInfo == null)
        {
            //could be caused by either saving tabContent at the same time the tab has been created
            //or by restarting the server while the web app stays open (fileIds are re-generated on server start)
            throw new Exception("Attempted to save TabContent with a FileId unknown to the server");
        }
        var filename = info.TabInfos.First(z => z.FileId == tabContent.FileId).Filename;
        var fullPath = _fileBasePath + filename;
        System.IO.FileInfo fileInfo = new System.IO.FileInfo(fullPath);
        fileInfo.Directory!.Create();
        System.IO.File.WriteAllText(fileInfo.FullName, tabContent.Text);
    }

    public Info GetInfo()
    {
        if (_info != null)
        {
            return _info;
        }
        var filenames = Directory.GetFiles(_fileBasePath).Select(z => Path.GetFileName(z)).Where(z => z != _fileOrderName);
        var tabInfos = new List<TabInfo>();
        Guid? activeFileId = null;
        if (System.IO.File.Exists(_fileBasePath + _fileOrderName))
        {
            var fileOrders = File.ReadAllLines(_fileBasePath + _fileOrderName);
            foreach (var filename in fileOrders)
            {
                //The active file will have a ! before the filename
                var isActive = filename.StartsWith('!');
                var fixedFilename = isActive ? filename.Substring(1) : filename;
                //if they created a new tab but never wrote any text, the file won't be created, but we should still perist the tab across server restarts
                if (filenames.Any(z => z == fixedFilename) || Regex.IsMatch(fixedFilename, @"^new \d+$"))
                {
                    var tabInfo = new TabInfo { FileId = Guid.NewGuid(), Filename = fixedFilename };
                    tabInfos.Add(tabInfo);
                    activeFileId ??= tabInfo.FileId;
                }
            }
        }
        if (filenames.Count() != tabInfos.Count()) {
            //there's either no fileorder file, or the fileorder data is incomplete (new files were added outside the app)
            foreach(var filename in filenames)
            {
                if (!tabInfos.Any(z => z.Filename == filename))
                {
                    tabInfos.Add(new TabInfo { FileId = Guid.NewGuid(), Filename = filename });
                }
            }
        }
        if (!tabInfos.Any())
        {
            tabInfos.Add(new TabInfo { FileId = Guid.NewGuid(), Filename = "new 1" });
        }
        if (!activeFileId.HasValue)
        {
            activeFileId = tabInfos[0].FileId;
        }
        var info = new Info
        {
            ActiveFileId = activeFileId.Value,
            TabInfos = tabInfos
        };
        _info = info;
        return new Info
        {
            ActiveFileId = activeFileId.Value,
            TabInfos = tabInfos
        };
    }

    public void SaveInfo(Info newInfo)
    {
        var existingInfo = GetInfo();
        var tabsToDelete = existingInfo.TabInfos.Where(z => !newInfo.TabInfos.Any(zz => zz.FileId == z.FileId));
        foreach (var tabToDelete in tabsToDelete)
        {
            var fullPath = _fileBasePath + tabToDelete.Filename;
            if (File.Exists(fullPath)) //check if tab was deleted
            {
                System.IO.File.Delete(fullPath);
            }
        }
        foreach (var newTab in newInfo.TabInfos)
        {
            var existingTab = existingInfo.TabInfos.FirstOrDefault(z => z.FileId == newTab.FileId);
            if (existingTab != null && existingTab.Filename != newTab.Filename) //check if file was renamed
            {
                var oldPath = _fileBasePath + existingTab.Filename;
                var newPath = _fileBasePath + newTab.Filename;
                if (File.Exists(oldPath))
                {
                    System.IO.File.Move(oldPath, newPath);
                }
            }
        }
        _info = newInfo;
        SaveFileOrderToDisk(newInfo);
    }


    private void SaveFileOrderToDisk(Info info){
        var lines = new List<string>();
        foreach(var tabInfo in info.TabInfos)
        {
            var line = tabInfo.Filename;
            if (tabInfo.FileId == info.ActiveFileId)
            {
                line = "!" + line;
            }
            lines.Add(line);
        }
        System.IO.FileInfo fileInfo = new System.IO.FileInfo(_fileBasePath + _fileOrderName);
        fileInfo.Directory!.Create();
        System.IO.File.WriteAllLines(fileInfo.FullName, lines);
    }
}