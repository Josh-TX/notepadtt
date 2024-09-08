using System.Collections.Concurrent;
using System.Text.RegularExpressions;

public class InfoStateService
{
    private Info? _info;
    private ConcurrentDictionary<string, DateTime> _fileLastCreatedDict = new ConcurrentDictionary<string, DateTime>();

    public Info GetInfo()
    {
        if (_info != null)
        {
            return _info.DeepCopy();
        }
        //_info is only null when the server just booted up.

        //The source of truth is ultimately the current state of the DATA_BASEPATH directory
        //However, we also want to preserve file order, active tab, and isProtected tabs based on the tab-data-file

        var tabInfos = new List<TabInfo>();

        var existingFilenames = Directory.Exists(Constants.DATA_BASEPATH)
            ? Directory.GetFiles(Constants.DATA_BASEPATH).Select(z => Path.GetFileName(z)).Where(z => z != Constants.TAB_DATA_FILENAME)
            : Enumerable.Empty<string>();
        Guid? activeFileId = null;
        var tabDataFileExists = System.IO.File.Exists(Path.Combine(Constants.DATA_BASEPATH, Constants.TAB_DATA_FILENAME));
        if (tabDataFileExists)
        {
            var tabDataLines = File.ReadAllLines(Path.Combine(Constants.DATA_BASEPATH, Constants.TAB_DATA_FILENAME));
            foreach (var tabDataLine in tabDataLines)
            {
                var fixedFilename = tabDataLine;
                var args = new List<string>();
                while (fixedFilename.StartsWith("\\") && fixedFilename.Length > 1)
                {
                    args.Add(fixedFilename.Substring(1, 1));
                    fixedFilename = fixedFilename.Substring(2);
                }
                var isActive = args.Contains("A");
                var isProtected = args.Contains("P");
                if (!existingFilenames.Any(z => z == fixedFilename))
                {
                    continue; //the file was removed while the server was down
                }
                var tabInfo = new TabInfo
                {
                    FileId = Guid.NewGuid(),
                    Filename = fixedFilename,
                    IsProtected = isProtected,
                };
                tabInfos.Add(tabInfo);
                activeFileId ??= tabInfo.FileId;
            }
        }

        if (existingFilenames.Count() != tabInfos.Count())
        {
            //files were added while the server was down
            foreach (var existingFilename in existingFilenames)
            {
                if (!tabInfos.Any(z => z.Filename == existingFilename))
                {
                    tabInfos.Add(new TabInfo { FileId = Guid.NewGuid(), Filename = existingFilename, IsProtected = true });
                }
            }
        }

        //if there aren't any files, we want to auto-create "new 1"
        if (!tabInfos.Any())
        {
            tabInfos.Add(new TabInfo { FileId = Guid.NewGuid(), Filename = "new 1", IsProtected = false });
            var fullPath = Path.Combine(Constants.DATA_BASEPATH, "new 1");
            System.IO.FileInfo fileInfo = new System.IO.FileInfo(fullPath);
            fileInfo.Directory!.Create();
            System.IO.File.WriteAllText(fileInfo.FullName, "\n\n\n\n");
        }
        if (!activeFileId.HasValue)
        {
            activeFileId = tabInfos[0].FileId;
        }
        var info = new Info
        {
            ActiveFileId = activeFileId.Value,
            TabInfos = tabInfos,
            ChangeToken = Guid.NewGuid()
        };
        _info = info;
        if (!tabDataFileExists)
        {
            //there are 2 reasons we need to save this to disk now (as opposed to waiting for updateInfo to be called)
            //1. We want to verify we have write permission to the data directory (possible to not have permissions with file mounts)
            //2. If the server restarts without updateInfo() being called, then the auto-created "new 1" would become protected
            SaveTabDataToDisk(_info);
        }
        return _info.DeepCopy();
    }

    /// <summary>
    /// Updates info to match the data directory (only if _info is non-null)
    /// </summary>
    public bool UpdateInfoToMatchData()
    {
        if (_info == null)
        {
            return false;
        }
        //we've already generated the FileIds, so now we're just detecting changes to the underlying filesystem (file added or file removed)
        var existingFilenames = Directory.Exists(Constants.DATA_BASEPATH)
            ? Directory.GetFiles(Constants.DATA_BASEPATH).Select(z => Path.GetFileName(z)).Where(z => z != Constants.TAB_DATA_FILENAME)
            : Enumerable.Empty<string>();
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
        var anyChanges = untrackedFilenames.Any() || deletedTabInfos.Any();
        if (anyChanges)
        {
            _info.ChangeToken = Guid.NewGuid();
            SaveTabDataToDisk(_info);
        }
        return anyChanges;
    }

    /// <summary>
    /// Updates the data directory to match the info (such as deleting & renaming files)
    /// </summary>
    public void UpdateDataToMatchInfo(Info newInfo)
    {
        var existingInfo = GetInfo();
        if (existingInfo.ChangeToken != newInfo.ChangeToken)
        {
            throw new ArgumentException("Stale ChangeToken");
        }
        var tabsToDelete = existingInfo.TabInfos.Where(z => !newInfo.TabInfos.Any(zz => zz.FileId == z.FileId));
        foreach (var tabToDelete in tabsToDelete)
        {
            var fullPath = Path.Combine(Constants.DATA_BASEPATH, tabToDelete.Filename);
            if (File.Exists(fullPath)) //check if tab was deleted
            {
                System.IO.File.Delete(fullPath);
            }
        }
        foreach (var newTab in newInfo.TabInfos)
        {
            if (newTab.Filename == Constants.TAB_DATA_FILENAME)
            {
                throw new ArgumentException("invalid filename");
            }
            var existingTab = existingInfo.TabInfos.FirstOrDefault(z => z.FileId == newTab.FileId);
            if (existingTab != null)
            {
                if (existingTab.Filename != newTab.Filename) //file was renamed
                {
                    var oldPath = Path.Combine(Constants.DATA_BASEPATH, existingTab.Filename);
                    var newPath = Path.Combine(Constants.DATA_BASEPATH, newTab.Filename);
                    if (File.Exists(oldPath))
                    {
                        System.IO.File.Move(oldPath, newPath);
                    }
                }
            } else
            {
                var fullPath = Path.Combine(Constants.DATA_BASEPATH, newTab.Filename);
                if (Regex.IsMatch(newTab.Filename, @"^new \d+$") && !File.Exists(fullPath)) //new tab was created, so create a file too
                {
                    System.IO.FileInfo fileInfo = new System.IO.FileInfo(fullPath);
                    fileInfo.Directory!.Create();
                    System.IO.File.WriteAllText(fileInfo.FullName, "\n\n\n\n");
                    _fileLastCreatedDict[newTab.Filename] = DateTime.UtcNow;
                }
            }
        }
        newInfo.ChangeToken = Guid.NewGuid();
        _info = newInfo;
        SaveTabDataToDisk(newInfo);
    }


    /// <summary>
    /// Returns true if we need to update all the clients with new info
    /// </summary>
    public bool FileRenamed(string oldFilename, string newFilename)
    {
        if (_info == null)
        {
            return false;
        }
        var updateNeeded = false;
        var affectedTabInfo = _info.TabInfos.FirstOrDefault(z => z.Filename == oldFilename);
        //if the file was renamed via the UI, affectedTabInfo should be null (since the info was already updated)
        if (affectedTabInfo != null)
        {
            //the file must've been renamed outside of the app.
            //UpdateInfo() updates the data to match the info... but in this case we need to update the info to match the data
            affectedTabInfo.Filename = newFilename;
            _info.ChangeToken = Guid.NewGuid();
            updateNeeded = true;
        }
        return updateNeeded;
    }

    /// <summary>
    /// Returns true if we need to update all the clients with new info
    /// </summary>
    public bool NotifyFileCreated(string createdFilename)
    {
        if (_info == null)
        {
            return false;
        }
        var updateNeeded = false;
        var createdTabInfo = _info.TabInfos.FirstOrDefault(z => z.Filename == createdFilename);
        //if the file was created via the UI, createdTabInfo should be non-null (since the info was already updated)
        if (createdTabInfo == null)
        {
            //the file must've been deleted outside of the app.
            //UpdateInfo() updates the data to match the info... but in this case we need to update the info to match the data
            _info.TabInfos.Add(new TabInfo { FileId = Guid.NewGuid(), Filename = createdFilename, IsProtected = true});
            _info.ChangeToken = Guid.NewGuid();
            UpdateActiveTab(); //if there were no tabInfos, this'll make the newly-added one active
            updateNeeded = true;
        }
        return updateNeeded;
    }


    /// <summary>
    /// Returns true if we need to update all the clients with new info
    /// </summary>
    public bool NotifyFileDeleted(string deletedFilename)
    {
        if (_info == null)
        {
            return false;
        }
        var updateNeeded = false;
        var tabInfoToDelete = _info.TabInfos.FirstOrDefault(z => z.Filename == deletedFilename);
        //if the file was deleted via the UI, tabInfoToDelete should be null (since the info was already updated)
        if (tabInfoToDelete != null)
        {
            //the file must've been deleted outside of the app.
            //UpdateInfo() updates the data to match the info... but in this case we need to update the info to match the data
            _info.TabInfos.Remove(tabInfoToDelete);
            _info.ChangeToken = Guid.NewGuid();
            UpdateActiveTab();
            updateNeeded = true;
        }
        return updateNeeded;
    }

    /// <summary>
    /// Returns true if we the file was just created via the UI
    /// </summary>
    public bool WasFileJustCreated(string filename)
    {
        var entryExists = _fileLastCreatedDict.TryGetValue(filename, out var updateDate);
        if (!entryExists)
        {
            return false; //the file hasn't been edited by the UI since the server started
        }
        var timespan = DateTime.UtcNow - updateDate;
        //if the file was edited within the last second, I'll assume that FileWatcher was triggered by the UI
        return timespan.TotalMilliseconds < 1000;
    }

    private void UpdateActiveTab()
    {
        var tabInfos = _info!.TabInfos;
        if (tabInfos.Any(z => z.FileId == _info.ActiveFileId)){
            //the ActiveFileId already references a tabinfo
        }
        else if (tabInfos.Any())
        {
            _info.ActiveFileId = tabInfos.Last().FileId;
        } else
        {
            _info.ActiveFileId = null;
        }
    }


    private void SaveTabDataToDisk(Info info){
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
        System.IO.FileInfo fileInfo = new System.IO.FileInfo(Path.Combine(Constants.DATA_BASEPATH, Constants.TAB_DATA_FILENAME));
        fileInfo.Directory!.Create();
        System.IO.File.WriteAllLines(fileInfo.FullName, lines);
    }
}