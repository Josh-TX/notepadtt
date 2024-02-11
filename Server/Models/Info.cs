public class Info {
    public required Guid? ActiveFileId { get; set; } 
    public required IEnumerable<TabInfo> TabInfos { get; set; } 
}