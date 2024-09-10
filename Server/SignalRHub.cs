using Microsoft.AspNetCore.SignalR;

public class SignalRHub : Hub
{
    private InfoStateService _infoStateService;
    private TabContentService _tabContentService;
    private TabSubscriptionService _tabSubscriptionService;
    private ILogger _logger;

    public SignalRHub(
        ILogger<SignalRHub> logger,
        InfoStateService infoStateService,
        TabContentService tabContentService,
        TabSubscriptionService tabSubscriptionService) : base()
    {
        _logger = logger;
        _infoStateService = infoStateService;
        _tabContentService = tabContentService;
        _tabSubscriptionService = tabSubscriptionService;
    }

    public override async Task OnConnectedAsync()
    {
        Info? info;
        var broadcastAll = false;
        try
        {
            //since this function is called on refresh, we want to verify that Info accurately matches disk contents
            broadcastAll = _infoStateService.UpdateInfoToMatchDisk();
            info = _infoStateService.GetInfo();
        }
        catch (Exception e)
        {
            _logger.LogError(e.Message.ToString());
            var fileId = Guid.NewGuid();
            info = new Info()
            {
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
        }
        if (broadcastAll)
        {
            await Clients.All.SendAsync("info", info);
        } else
        {
            await Clients.Caller.SendAsync("info", info);
        }
    }
    public override Task OnDisconnectedAsync(Exception? exception)
    {
        try
        {
            _tabSubscriptionService.RemoveAll(Context.ConnectionId);
        }
        catch (Exception e)
        {
            _logger.LogError(e.Message.ToString());
        }
        return base.OnDisconnectedAsync(exception);
    }

    public async Task InfoChanged(Info Info)
    {
        try
        {
            _infoStateService.UpdateDataToMatchInfo(Info);
            await Clients.All.SendAsync("info", Info);
        }
        catch (Exception e)
        {
            _logger.LogError(e.Message.ToString());
        }
    }

    public void UnsubscribeTabContent(Guid fileId)
    {
        try
        {
            _tabSubscriptionService.Remove(fileId, Context.ConnectionId);
        }
        catch (Exception e)
        {
            _logger.LogError(e.Message.ToString());
        }
    }

    public async Task SubscribeTabContent(Guid fileId)
    {
        try
        {
            _tabSubscriptionService.Add(fileId, Context.ConnectionId);
            var tabContent = _tabContentService.LoadTabContent(fileId);
            await Clients.Caller.SendAsync("tabContent", tabContent);
        }
        catch (Exception e)
        {
            _logger.LogError(e.Message.ToString());
        }
    }

    public async Task TabContentChanged(TabContent tabContent)
    {
        try
        {
            _tabContentService.SaveTabContent(tabContent);
            //find all subscriptions for this fileId excluding the Caller ConnectionId
            var connectionIds = _tabSubscriptionService.GetConnectionIds(tabContent.FileId).Where(z => z != Context.ConnectionId);
            await Clients.Clients(connectionIds).SendAsync("tabContent", tabContent);
        }
        catch (Exception e)
        {
            _logger.LogError(e.Message.ToString());
        }
    }
}