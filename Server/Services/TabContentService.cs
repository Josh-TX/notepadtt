using System.Collections.Concurrent;
using System.Text.RegularExpressions;

public class TabContentService
{
    private InfoStateService _infoStateService;
    private ConcurrentDictionary<string, DateTime> _fileLastUpdateDict;

    public TabContentService(InfoStateService infoStateService)
    {
        _infoStateService = infoStateService;
        _fileLastUpdateDict = new ConcurrentDictionary<string, DateTime>();
    }

    public bool WasFileChangedViaUI(string filename)
    {
        var entryExists = _fileLastUpdateDict.TryGetValue(filename, out var updateDate);
        if (!entryExists)
        {
            return false; //the file hasn't been edited by the UI since the server started
        }
        var timespan = DateTime.UtcNow - updateDate;
        //if the file was edited within the last second, I'll assume that FileWatcher was triggered by the UI
        return timespan.TotalMilliseconds < 1000;
    }

    public TabContent LoadTabContent(Guid fileId)
    {
        var info = _infoStateService.GetInfo();
        var tabInfo = info.TabInfos.FirstOrDefault(z => z.FileId == fileId);
        if (tabInfo == null)
        {
            return new TabContent
            {
                FileId = fileId,
                Text = "\n\n\n\n"
            };
        }
        var fullPath = Path.Combine(Constants.DATA_BASEPATH, tabInfo.Filename);
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
        var info = _infoStateService.GetInfo();
        var tabInfo = info.TabInfos.FirstOrDefault(z => z.FileId == tabContent.FileId);
        if (tabInfo == null)
        {
            //could be caused by either saving tabContent at the same time the tab has been created
            //or by restarting the server while the web app stays open (fileIds are re-generated on server start)
            throw new Exception("Attempted to save TabContent with a FileId unknown to the server");
        }
        var filename = info.TabInfos.First(z => z.FileId == tabContent.FileId).Filename;
        _fileLastUpdateDict[filename] = DateTime.UtcNow;
        var fullPath = Path.Combine(Constants.DATA_BASEPATH, filename);
        System.IO.FileInfo fileInfo = new System.IO.FileInfo(fullPath);
        fileInfo.Directory!.Create();
        System.IO.File.WriteAllText(fileInfo.FullName, tabContent.Text);

    }
}