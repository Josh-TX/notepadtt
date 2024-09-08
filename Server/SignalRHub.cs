using Microsoft.AspNetCore.SignalR;

public class SignalRHub : Hub
{
    private InfoStateService _infoStateService;
    private TabContentService _tabContentService;
    private TabSubscriptionService _tabSubscriptionService;

    public SignalRHub(InfoStateService infoStateService, TabContentService tabContentService, TabSubscriptionService tabSubscriptionService) : base()
    {
        _infoStateService = infoStateService;
        _tabContentService = tabContentService;
        _tabSubscriptionService = tabSubscriptionService;
    }

    public override async Task OnConnectedAsync()
    {
        Info? info;
        try {
            var updateNeeded = _infoStateService.UpdateInfoToMatchData();
            info = _infoStateService.GetInfo();
        } catch(Exception e){
            if (e is UnauthorizedAccessException){
                var fileId = Guid.NewGuid();
                info = new Info() {
                    ActiveFileId = fileId,
                    ChangeToken = Guid.Empty,
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
        _tabSubscriptionService.RemoveAll(Context.ConnectionId);
        return base.OnDisconnectedAsync(exception);
    }

    public async Task InfoChanged(Info Info)
    {
        _infoStateService.UpdateDataToMatchInfo(Info);
        await Clients.All.SendAsync("info", Info);
    }

    public void UnsubscribeTabContent(Guid fileId)
    {
        _tabSubscriptionService.Remove(fileId, Context.ConnectionId);
    }

    public async Task SubscribeTabContent(Guid fileId)
    {
        _tabSubscriptionService.Add(fileId, Context.ConnectionId);
        var tabContent = _tabContentService.LoadTabContent(fileId);
        await Clients.Caller.SendAsync("tabContent", tabContent);
    }

    public async Task TabContentChanged(TabContent tabContent)
    {
        _tabContentService.SaveTabContent(tabContent);
        //find all subscriptions for this fileId excluding the Caller ConnectionId
        var connectionIds = _tabSubscriptionService.GetConnectionIds(tabContent.FileId).Where(z => z != Context.ConnectionId);
        var tasks = connectionIds.Select(z => Clients.Client(z).SendAsync("tabContent", tabContent)).ToList();
        await Task.WhenAll(tasks);
    }
}