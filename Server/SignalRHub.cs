using Microsoft.AspNetCore.SignalR;

public class SignalRHub : Hub
{
    public SignalRHub() : base(){

    }
    private static List<Subscription> _subscriptions = new List<Subscription>();

    StorageManager storageManager = new StorageManager();

    public override async Task OnConnectedAsync()
    {
        Info? info = null;
        try {
            info = storageManager.GetInfo();
        } catch(Exception e){
            if (e is UnauthorizedAccessException){
                var fileId = Guid.NewGuid();
                info = new Info() {
                    ActiveFileId = fileId,
                    TabInfos = new List<TabInfo> {
                        new TabInfo() {
                            FileId = fileId,
                            Filename = "SERVER ERROR: " + e.Message.ToString(),
                            IsProtected = true
                        }
                    }
                };
            } else {
                throw;
            }
        }
        await Clients.Caller.SendAsync("info", info);
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