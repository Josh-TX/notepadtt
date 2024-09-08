public class Info
{
    public required Guid? ActiveFileId { get; set; }
    /// <summary>
    /// The order determines the order they appear in the app
    /// </summary>
    public required List<TabInfo> TabInfos { get; set; }

    /// <summary>
    /// Used to identify stale infos when an update is attempted
    /// </summary>
    public required Guid ChangeToken { get; set; }

    public Info DeepCopy()
    {
        return new Info {  
            ActiveFileId = ActiveFileId, 
            TabInfos = TabInfos.Select(z => new TabInfo { FileId = z.FileId, Filename = z.Filename, IsProtected = z.IsProtected}).ToList(), 
            ChangeToken = ChangeToken 
        };
    }
}




public class TabInfo
{
    /// <summary>
    /// The name of the file on disk. Clients should display this in the tab
    /// </summary>
    public required string Filename { get; set; }

    /// <summary>
    /// a temporary identifier that the clients should use to identify files (particularly important when a file is renamed)
    /// </summary>
    public required Guid FileId { get; set; }

    /// <summary>
    /// When true, the web UI has additional measures to prevent accidental deletion
    /// </summary>
    public required bool IsProtected { get; set; }
}


public class TabContent
{
    public required Guid FileId { get; set; }
    public required string Text { get; set; }
}