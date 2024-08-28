public class Info
{
    public required Guid? ActiveFileId { get; set; }
    /// <summary>
    /// The order determines the order they appear in the app
    /// </summary>
    public required IEnumerable<TabInfo> TabInfos { get; set; }
}




public class TabInfo
{
    /// <summary>
    /// The name of the file on disk. Clients should display this
    /// </summary>
    public required string Filename { get; set; }

    /// <summary>
    /// a temporary identifier that the clients should use to identify files (particularly important when a file is renamed)
    /// </summary>
    public required Guid FileId { get; set; }
}
public class TabContent
{
    public required Guid FileId { get; set; }
    public required string Text { get; set; }
}