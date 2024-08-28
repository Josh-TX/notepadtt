using System.Security.Cryptography;
using System.Text.Json;
using System.Text.RegularExpressions;

public class StorageManager
{
#if !DEBUG
    private const string _fileBasePath = "/data/";
#else
    private const string _fileBasePath = "data/";
#endif
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
        //since this is called a bunch, I don't want to call GetInfo() since that always scans the 
        var info = _info != null ? _info : GetInfo();
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
        var existingFilenames = Directory.GetFiles(_fileBasePath).Select(z => Path.GetFileName(z)).Where(z => z != _fileOrderName);
        if (_info != null)
        {
            //we've already generated the FileIds, so now we're just detecting changes to the underlying filesystem
            var untrackedFilenames = existingFilenames.Except(_info.TabInfos.Select(z => z.Filename));
            foreach (var untrackedFilename in untrackedFilenames)
            {
                _info.TabInfos.Add(new TabInfo { FileId = Guid.NewGuid(), Filename = untrackedFilename, IsProtected = true });
            }
            var deletedTabInfos = _info.TabInfos.Where(tabInfo => !existingFilenames.Contains(tabInfo.Filename)).ToList();
            foreach (var deletedTabInfo in deletedTabInfos)
            {
                _info.TabInfos.Remove(deletedTabInfo);
            }
            return _info;
        }
        var tabInfos = new List<TabInfo>();
        Guid? activeFileId = null;
        if (System.IO.File.Exists(_fileBasePath + _fileOrderName))
        {
            var fileOrders = File.ReadAllLines(_fileBasePath + _fileOrderName);
            foreach (var filename in fileOrders)
            {
                var fixedFilename = filename;
                var args = new List<string>();
                while (filename.StartsWith("\\") && fixedFilename.Length > 1)
                {
                    args.Add(fixedFilename.Substring(1, 1));
                    fixedFilename = fixedFilename.Substring(2);
                }
                var isActive = args.Contains("A");
                var isProtected = args.Contains("P");
                //if they created a new tab but never wrote any text, the file won't be created, but we should still perist the tab across server restarts
                if (existingFilenames.Any(z => z == fixedFilename) || Regex.IsMatch(fixedFilename, @"^new \d+$"))
                {
                    var tabInfo = new TabInfo { 
                        FileId = Guid.NewGuid(),
                        Filename = fixedFilename,
                        IsProtected = isProtected,
                    };
                    tabInfos.Add(tabInfo);
                    activeFileId ??= tabInfo.FileId;
                }
            }
        }
        if (existingFilenames.Count() != tabInfos.Count()) {
            //there's either no fileorder file, or the fileorder data is incomplete (new files were added outside the app)
            foreach(var existingFilename in existingFilenames)
            {
                if (!tabInfos.Any(z => z.Filename == existingFilename))
                {
                    tabInfos.Add(new TabInfo { FileId = Guid.NewGuid(), Filename = existingFilename, IsProtected = true });
                }
            }
        }
        if (!tabInfos.Any())
        {
            tabInfos.Add(new TabInfo { FileId = Guid.NewGuid(), Filename = "new 1", IsProtected = false });
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
        return _info;
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
            var fullPath = _fileBasePath + newTab.Filename;
            if (Regex.IsMatch(newTab.Filename, @"^new \d+$") && !File.Exists(fullPath))
            {
                System.IO.FileInfo fileInfo = new System.IO.FileInfo(fullPath);
                fileInfo.Directory!.Create();
                System.IO.File.WriteAllText(fileInfo.FullName, "\n\n\n\n");
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
            if (tabInfo.IsProtected)
            {
                line = "\\P" + line;
            }
            if (tabInfo.FileId == info.ActiveFileId)
            {
                line = "\\A" + line;
            }
            lines.Add(line);
        }
        System.IO.FileInfo fileInfo = new System.IO.FileInfo(_fileBasePath + _fileOrderName);
        fileInfo.Directory!.Create();
        System.IO.File.WriteAllLines(fileInfo.FullName, lines);
    }
}