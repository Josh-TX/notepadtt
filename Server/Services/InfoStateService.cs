using System.Collections.Concurrent;
using System.Text.RegularExpressions;

public class InfoStateService
{
    private Info? _info;
    private ConcurrentDictionary<string, DateTime> _fileLastCreatedDict = new ConcurrentDictionary<string, DateTime>();
    private Regex? _includeRegex = null;
    private Regex? _excludeRegex = null;
    private int _maxkb;

    public InfoStateService()
    {
        var includePattern = Environment.GetEnvironmentVariable("INCLUDE_FILES");
        if (includePattern != null)
        {
            string regexStr = "^" + Regex.Escape(includePattern).Replace(@"\*", ".*").Replace(@"\?", ".") + "$";
            _includeRegex = new Regex(regexStr, RegexOptions.IgnoreCase);
        }
        var excludePattern = Environment.GetEnvironmentVariable("EXCLUDE_FILES");
        if (excludePattern != null)
        {
            string regexStr = "^" + Regex.Escape(excludePattern).Replace(@"\*", ".*").Replace(@"\?", ".") + "$";
            _excludeRegex = new Regex(regexStr, RegexOptions.IgnoreCase);
        }
        _maxkb = int.TryParse(Environment.GetEnvironmentVariable("MAXKB"), out var n) ? n : 200; //default to 200kb
    }

    public Info GetInfo()
    {
        if (_info != null)
        {
            return _info.DeepCopy();
        }
        //_info is only null when the server just booted up.

        //The source of truth is ultimately the current state of the DATA_BASEPATH directory
        //However, we also want to preserve file order, active tab, and isProtected tabs using the metadata file (if it exists)

        var tabInfos = new List<TabInfo>();

        var existingFilenames = Directory.Exists(Constants.DATA_BASEPATH)
            ? Directory.GetFiles(Constants.DATA_BASEPATH).Select(z => Path.GetFileName(z)).Where(z => z != Constants.METADATA_FILENAME)
            : Enumerable.Empty<string>();
        Guid? activeFileId = null;
        var metadataFileExists = System.IO.File.Exists(Path.Combine(Constants.DATA_BASEPATH, Constants.METADATA_FILENAME));
        if (metadataFileExists)
        {
            var metadataLines = File.ReadAllLines(Path.Combine(Constants.DATA_BASEPATH, Constants.METADATA_FILENAME));
            foreach (var metadataLine in metadataLines)
            {
                var fixedFilename = metadataLine;
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
                if (tabInfos.Any(z => z.Filename == fixedFilename))
                {
                    continue; //just in case the metadataLines contained duplicates.
                }
                var tabInfo = new TabInfo
                {
                    FileId = Guid.NewGuid(),
                    Filename = fixedFilename,
                    IsProtected = isProtected,
                };
                tabInfos.Add(tabInfo);
                if (isActive)
                {
                    activeFileId ??= tabInfo.FileId;
                }
            }
        }

        if (existingFilenames.Count() != tabInfos.Count())
        {
            //files were added while the server was down
            foreach (var existingFilename in existingFilenames)
            {
                if (!tabInfos.Any(z => z.Filename == existingFilename) && ShouldTrackFile(existingFilename))
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
        if (!metadataFileExists)
        {
            //there are 2 reasons we need to save this to disk now (as opposed to waiting for UpdateInfoToMatchDisk to be called)
            //1. We want to verify we have write permission to the data directory (possible to not have permissions with file mounts)
            //2. If the server restarts without UpdateInfoToMatchDisk() being called, then the auto-created "new 1" would become protected
            SaveMetadataToDisk(_info);
        }
        return _info.DeepCopy();
    }

    /// <summary>
    /// Updates info to match the disk (only if _info is non-null). Returns 
    /// </summary>
    public bool UpdateInfoToMatchDisk()
    {
        if (_info == null)
        {
            return false;
        }
        var existingFilenames = Directory.Exists(Constants.DATA_BASEPATH)
            ? Directory.GetFiles(Constants.DATA_BASEPATH).Select(z => Path.GetFileName(z)).Where(z => z != Constants.METADATA_FILENAME)
            : Enumerable.Empty<string>();
        var untrackedFilenames = existingFilenames.Except(_info.TabInfos.Select(z => z.Filename)).Where(ShouldTrackFile);
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
            SaveMetadataToDisk(_info);
        }
        return anyChanges;
    }

    /// <summary>
    /// Updates the disk to match the info (such as deleting & renaming files)
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
            if (newTab.Filename == Constants.METADATA_FILENAME)
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
        SaveMetadataToDisk(newInfo);
    }


    /// <summary>
    /// Returns true if we need to broadcast to all the clients the new info
    /// </summary>
    public (bool infoBroadcastNeeded, Guid? replacedFileId) NotifyFileRenamed(string oldFilename, string newFilename)
    {
        if (_info == null)
        {
            return (false, null);
        }
        var infoBroadcastNeeded = false;
        Guid? replacedFileId = null;
        var oldNameTabInfo = _info.TabInfos.FirstOrDefault(z => z.Filename == oldFilename);
        var newNameTabInfo = _info.TabInfos.FirstOrDefault(z => z.Filename == newFilename);
        if (oldNameTabInfo != null)
        {
            //the info was already tracking this file, but it wasn't renamed via the UI
            _info.ChangeToken = Guid.NewGuid();
            infoBroadcastNeeded = true;
            if (newNameTabInfo != null)
            {
                //oldFile must have replaced newFile
                _info.TabInfos.Remove(oldNameTabInfo);
                //since there could be some active connections to newNameTabInfo, we also might need to broadcast the tabContents
                replacedFileId = newNameTabInfo.FileId;
            } else
            {
                //traditional rename of a file
                oldNameTabInfo.Filename = newFilename;
            }
        } else
        {
            if (newNameTabInfo != null)
            {
                //the file must've been renamed via the UI, and so no need to do anything more
            } else {
                //this file was not being tracked at all. Either the file shoudln't be tracked(), or perhaps it was moved here from outside the /data directory
                if (ShouldTrackFile(newFilename))
                {
                    _info.TabInfos.Add(new TabInfo { FileId = Guid.NewGuid(), Filename = newFilename, IsProtected = true });
                    _info.ChangeToken = Guid.NewGuid();
                    infoBroadcastNeeded = true;
                }
            }
        }
        if (infoBroadcastNeeded)
        {
            SaveMetadataToDisk(_info);
        }
        return (infoBroadcastNeeded, replacedFileId);
    }

    /// <summary>
    /// Returns true if we need to broadcast the info to all clients
    /// </summary>
    public bool NotifyFileCreated(string createdFilename)
    {
        if (_info == null)
        {
            return false;
        }
        var infoBroadcastNeeded = false;
        var createdTabInfo = _info.TabInfos.FirstOrDefault(z => z.Filename == createdFilename);
        //if the file was created via the UI, createdTabInfo would be non-null
        if (createdTabInfo == null)
        {
            //the file must've been created not via the UI
            if (ShouldTrackFile(createdFilename))
            {
                _info.TabInfos.Add(new TabInfo { FileId = Guid.NewGuid(), Filename = createdFilename, IsProtected = true});
                _info.ChangeToken = Guid.NewGuid();
                UpdateActiveTab(); //if there were no tabInfos, this'll make the newly-added one active
                infoBroadcastNeeded = true;
                SaveMetadataToDisk(_info);
            }

        }
        return infoBroadcastNeeded;
    }


    /// <summary>
    /// Returns true if we need to broadcast the info to all clients
    /// </summary>
    public bool NotifyFileDeleted(string deletedFilename)
    {
        if (_info == null)
        {
            return false;
        }
        var infoBroadcastNeeded = false;
        var tabInfoToDelete = _info.TabInfos.FirstOrDefault(z => z.Filename == deletedFilename);
        //if the file was deleted via the UI, tabInfoToDeleteshould would be null
        if (tabInfoToDelete != null)
        {
            //the file must've been deleted not via the UI
            _info.TabInfos.Remove(tabInfoToDelete);
            _info.ChangeToken = Guid.NewGuid();
            UpdateActiveTab();
            infoBroadcastNeeded = true;
            SaveMetadataToDisk(_info);
        }
        return infoBroadcastNeeded;
    }

    /// <summary>
    /// Returns true if we the file was just created via the UI
    /// </summary>
    public bool WasFileCreatedViaUI(string filename)
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

    private void SaveMetadataToDisk(Info info)
    {
        var lines = new List<string>();
        foreach (var tabInfo in info.TabInfos)
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
        System.IO.FileInfo fileInfo = new System.IO.FileInfo(Path.Combine(Constants.DATA_BASEPATH, Constants.METADATA_FILENAME));
        fileInfo.Directory!.Create();
        System.IO.File.WriteAllLines(fileInfo.FullName, lines);
    }

    private bool ShouldTrackFile(string filename)
    {
        if (_includeRegex != null && !_includeRegex.IsMatch(filename))
        {
            return false;
        }
        if (_excludeRegex != null && _excludeRegex.IsMatch(filename))
        {
            return false;
        }
        var fileInfo = new System.IO.FileInfo(Path.Combine(Constants.DATA_BASEPATH, filename));
        double kb = fileInfo.Length / 1024d;
        if (kb > _maxkb)
        {
            return false;
        }
        return true;
    }
}