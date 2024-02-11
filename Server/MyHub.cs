using Microsoft.AspNetCore.SignalR;

public class MyHub : Hub
{
    public MyHub() : base(){

    }
    private static List<Subscription> _subscriptions = new List<Subscription>();

    StorageManager storageManager = new StorageManager();

    public override async Task OnConnectedAsync()
    {
        await Clients.Caller.SendAsync("info", storageManager.GetInfo());
    }
    public override Task OnDisconnectedAsync(Exception? exception)
    {
        _subscriptions.RemoveAll(z => z.ConnectionId == Context.ConnectionId);
        return base.OnDisconnectedAsync(exception);
    }

    public async Task InfoChanged(Info Info)
    {
        storageManager.SaveInfo(Info);
        await Clients.All.SendAsync("info", Info);
    }

    public void UnsubscribeTabContent(Guid fileId)
    {
        _subscriptions.RemoveAll(z => z.FileId == fileId && z.ConnectionId == Context.ConnectionId);
    }

    public async Task SubscribeTabContent(Guid fileId)
    {
        //removeAll to prevent duplicates
        _subscriptions.RemoveAll(z => z.FileId == fileId && z.ConnectionId == Context.ConnectionId);
        _subscriptions.Add(new Subscription{
            FileId = fileId,
            ConnectionId = Context.ConnectionId
        });
        var tabContent = storageManager.LoadTabContent(fileId);
        await Clients.Caller.SendAsync("tabContent", tabContent);
    }

    public async Task TabContentChanged(TabContent tabContent)
    {
        storageManager.SaveTabContent(tabContent);
        //find all subscriptions for this fileId excluding the Caller ConnectionId
        var subscriptions = _subscriptions.Where(z => z.FileId == tabContent.FileId && z.ConnectionId != Context.ConnectionId);
        var tasks = subscriptions.Select(z => Clients.Client(z.ConnectionId).SendAsync("tabContent", tabContent)).ToList();
        await Task.WhenAll(tasks);
    }

    private class Subscription {
        public required Guid FileId {get;set;}
        public required string ConnectionId {get;set;}
    }
}