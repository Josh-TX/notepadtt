using System.Text.Json;

public class StorageManager
{
    private const string _fileBasePath = "data/files/";
    private const string _infoPath = "data/info.json";
    private Info? _info;

    public TabContent LoadTabContent(Guid fileId)
    {
        var info = GetInfo();
        var tabInfo = info.TabInfos.FirstOrDefault(z => z.FileId == fileId);
        if (tabInfo == null){
            return new TabContent
            {
                FileId = fileId,
                Text = ""
            };
        }
        var fullPath = _fileBasePath + tabInfo.Filename;
        if (!System.IO.File.Exists(fullPath))
        {
            return new TabContent
            {
                FileId = fileId,
                Text = ""
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
        if (!System.IO.File.Exists(_infoPath))
        {
            var fileId = Guid.NewGuid();
            var info = new Info
            {
                ActiveFileId = fileId,
                TabInfos = [
                    new TabInfo {
                        Filename = "new 1",
                        FileId = fileId
                    }
                ]
            };
            SaveInfoToDisk(info);
            return info;
        }
        var json = System.IO.File.ReadAllText(_infoPath);
        _info = JsonSerializer.Deserialize<Info>(json)!;
        return _info;
    }

    public void SaveInfo(Info newInfo)
    {
        var existingInfo = GetInfo();
        var tabsToDelete = existingInfo.TabInfos.Where(z => !newInfo.TabInfos.Any(zz => zz.FileId == z.FileId));
        foreach (var tabToDelete in tabsToDelete)
        {
            var fullPath = _fileBasePath + tabToDelete.Filename;
            if (File.Exists(fullPath))
            {
                System.IO.File.Delete(fullPath);
            }
        }
        foreach (var newTab in newInfo.TabInfos)
        {
            var existingTab = existingInfo.TabInfos.FirstOrDefault(z => z.FileId == newTab.FileId);
            if (existingTab != null && existingTab.Filename != newTab.Filename)
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
        SaveInfoToDisk(newInfo);
    }

    private void SaveInfoToDisk(Info info){
        string json = JsonSerializer.Serialize(info);
        System.IO.FileInfo fileInfo = new System.IO.FileInfo(_infoPath);
        fileInfo.Directory!.Create();
        System.IO.File.WriteAllText(fileInfo.FullName, json);
    }
}