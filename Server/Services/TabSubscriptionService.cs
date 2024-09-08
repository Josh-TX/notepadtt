
public class TabSubscriptionService
{
    private static List<Subscription> _subscriptions = new List<Subscription>();


    private class Subscription
    {
        public required Guid FileId { get; set; }
        public required string ConnectionId { get; set; }
    }

    public IEnumerable<string> GetConnectionIds(Guid fileId)
    {
        return _subscriptions.Where(z => z.FileId == fileId).Select(z => z.ConnectionId);
    }

    public void Add(Guid fileId, string connectionId)
    {
        //removeAll to prevent duplicates
        _subscriptions.RemoveAll(z => z.FileId == fileId && z.ConnectionId == connectionId);
        _subscriptions.Add(new Subscription
        {
            FileId = fileId,
            ConnectionId = connectionId
        });
    }

    public void Remove(Guid fileId, string connectionId)
    {
        _subscriptions.RemoveAll(z => z.FileId == fileId && z.ConnectionId == connectionId);
    }

    public void RemoveAll(string connectionId)
    {
        _subscriptions.RemoveAll(z => z.ConnectionId == connectionId);
    }
}