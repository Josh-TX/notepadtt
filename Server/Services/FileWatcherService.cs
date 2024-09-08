using Microsoft.AspNetCore.SignalR;
using System.Collections.Concurrent;

public class FileWatcherService : BackgroundService 
{
    private readonly FileSystemWatcher _watcher;
    private readonly IHubContext<SignalRHub> _hubContext;
    private readonly InfoStateService _infoStateService;
    private readonly TabContentService _tabContentService;
    private readonly TabSubscriptionService _tabSubscriptionService;

    private ConcurrentDictionary<string, Guid> _fileChangeTokenDict = new ConcurrentDictionary<string, Guid>();

    public FileWatcherService(
        IHubContext<SignalRHub> hubContext, 
        InfoStateService infoStateService, 
        TabContentService tabContentService, 
        TabSubscriptionService tabSubscriptionService)
    {
        _hubContext = hubContext;
        _infoStateService = infoStateService;
        _tabContentService = tabContentService;
        _tabSubscriptionService = tabSubscriptionService;
        _fileChangeTokenDict = new ConcurrentDictionary<string, Guid>();
        _watcher = new FileSystemWatcher(Constants.DATA_BASEPATH)
        {
            NotifyFilter = NotifyFilters.LastWrite | NotifyFilters.FileName | NotifyFilters.DirectoryName,
            IncludeSubdirectories = false
        };

        _watcher.Changed += OnChanged;
        _watcher.Created += OnCreated;
        _watcher.Deleted += OnDeleted;
        _watcher.Renamed += OnRenamed;
    }

    //supposed to fire when the text changes
    private async void OnChanged(object source, FileSystemEventArgs e)
    {
        if (e.Name == null)
        {
            return; //not sure how this could happen
        }
        if (e.Name == Constants.TAB_DATA_FILENAME)
        {
            return;
        }
        var changedViaUI = _tabContentService.WasFileChangedViaUI(e.Name) || _infoStateService.WasFileJustCreated(e.Name);
        if (changedViaUI)
        {
            return; //since it was changed via the UI, signalR already updated other clients
        }
        var tabInfo = _infoStateService.GetInfo().TabInfos.FirstOrDefault(z => z.Filename == e.Name);
        if (tabInfo == null)
        {
            return; //somehow this file isn't being tracked, so no need to update clients about changes to a file it doesn't know about
        }
        var connectionIds = _tabSubscriptionService.GetConnectionIds(tabInfo.FileId);
        if (!connectionIds.Any())
        {
            return; //none of the clients are currently viewing the tab for this file
        }

        //When I use the real windows Notepad++ app to save a file I have open in notepadtt, there are 2 problems:
        //1. The FileWatcher triggers this function so fast that I get an exception calling ReadAllText because the file is in-use. 
        //2. This OnChanged method gets called twice even though just a single file is saved. I guess saving is not an atomic operation?
        // Because of these 2 problems, we need to delay and debounce the changes, hence the _fileChangeTokenDict
        var token = Guid.NewGuid();
        _fileChangeTokenDict[e.Name] = token;
        await Task.Delay(20); //in my testing, the two OnChanged calls happen less than 5ms apart, so 20ms should be plenty
        _fileChangeTokenDict.TryGetValue(e.Name, out var currentToken);
        if (token != currentToken)
        {
            return; //another change event for the same file occurred during the Task.Delay()
        }
        string? text = null;
        //and just in case the 20ms wasn't enough, I've got a try-catch loop
        for (var i = 0; i < 5; i++){
            try {
                text = System.IO.File.ReadAllText(e.FullPath);
                break;
            }
            catch (IOException) {
                if (i == 4){
                    throw;
                }
            }
            await Task.Delay(50 + 150 * i);
            _fileChangeTokenDict.TryGetValue(e.Name, out currentToken);
            if (token != currentToken)
            {
                return; //another change event for the same file occurred during the Task.Delay()
            }
        }
        var tabContent = new TabContent
        {
            FileId = tabInfo.FileId,
            Text = text!
        };
        await _hubContext.Clients.Clients(connectionIds).SendAsync("tabContent", tabContent);
    }

    private async void OnCreated(object source, FileSystemEventArgs e)
    {
        if (e.Name == null)
        {
            return; //not sure how this could happen
        }
        if (e.Name == Constants.TAB_DATA_FILENAME){
            return;
        }
        var updateNeeded = _infoStateService.NotifyFileCreated(e.Name);
        if (updateNeeded)
        {
            await _hubContext.Clients.All.SendAsync("Info", _infoStateService.GetInfo());
        }
    }

    private async void OnDeleted(object source, FileSystemEventArgs e)
    {
        if (e.Name == null)
        {
            return; //not sure how this could happen
        }
        //we don't need to check for TAB_DATA_FILENAME
        var updateNeeded = _infoStateService.NotifyFileDeleted(e.Name);
        if (updateNeeded)
        {
            await _hubContext.Clients.All.SendAsync("Info", _infoStateService.GetInfo());
        }
    }

    private async void OnRenamed(object source, RenamedEventArgs e)
    {
        if (!System.IO.File.Exists(e.FullPath))
        {
            return; //a directory was renamed
        }
        if (e.OldName == null || e.Name == null)
        {
            return; //not sure how this could happen
        }
        var updateNeeded = _infoStateService.FileRenamed(e.OldName, e.Name);
        if (updateNeeded)
        {
            await _hubContext.Clients.All.SendAsync("Info", _infoStateService.GetInfo());
        }
    }

    protected override Task ExecuteAsync(CancellationToken stoppingToken)
    {
        _watcher.EnableRaisingEvents = true;
        // Keep the service running until the application is shutting down
        return Task.CompletedTask;
    }

    public override Task StopAsync(CancellationToken cancellationToken)
    {
        _watcher.EnableRaisingEvents = false;
        _watcher.Dispose();
        return base.StopAsync(cancellationToken);
    }
}
