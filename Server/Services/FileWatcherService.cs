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
        if (e.Name == Constants.METADATA_FILENAME)
        {
            return;
        }
        var changedViaUI = _tabContentService.WasFileChangedViaUI(e.Name) || _infoStateService.WasFileCreatedViaUI(e.Name);
        if (changedViaUI)
        {
            return; //since it was changed via the UI, signalR already updated other clients
        }
        var tabInfo = _infoStateService.GetInfo().TabInfos.FirstOrDefault(z => z.Filename == e.Name);
        if (tabInfo == null)
        {
            return; //somehow this file isn't being tracked, so no need to update clients about changes to a file it doesn't know about
        }
        await BroadcastFileChange(e.Name, tabInfo.FileId);
    }

    private async void OnCreated(object source, FileSystemEventArgs e)
    {
        if (e.Name == null)
        {
            return; //not sure how this could happen
        }
        if (e.Name == Constants.METADATA_FILENAME)
        {
            return;
        }
        var infoBroadcastNeeded = _infoStateService.NotifyFileCreated(e.Name);
        if (infoBroadcastNeeded)
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
        var infoBroadcastNeeded = _infoStateService.NotifyFileDeleted(e.Name);
        if (infoBroadcastNeeded)
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
        if (e.Name == Constants.METADATA_FILENAME)
        {
            return;
        }
        var tuple = _infoStateService.NotifyFileRenamed(e.OldName, e.Name);
        if (tuple.infoBroadcastNeeded)
        {
            await _hubContext.Clients.All.SendAsync("Info", _infoStateService.GetInfo());
        }
        if (tuple.replacedFileId.HasValue)
        {
            await BroadcastFileChange(e.Name, tuple.replacedFileId.Value);
        }
    }

    private async Task BroadcastFileChange(string filename, Guid fileId)
    {
        var connectionIds = _tabSubscriptionService.GetConnectionIds(fileId);
        if (!connectionIds.Any())
        {
            return; //none of the clients are currently viewing the tab for this file
        }
        //most apps seem to trigger multiple change events in quick succession (less than 5ms)
        //furthermore, there's often an error attempting to ReadAllText immediately after the 1st of several change events
        //therefore, I'll always delay 20 ms, and debounce to just the latest one
        var token = Guid.NewGuid();
        _fileChangeTokenDict[filename] = token;
        await Task.Delay(20);
        _fileChangeTokenDict.TryGetValue(filename, out var currentToken);
        if (token != currentToken)
        {
            return; //another change event for the same file occurred during the Task.Delay()
        }
        string? text = null;
        //because of the possible errors calling ReadAllText, we'll have some retry logic
        for (var i = 0; i < 5; i++)
        {
            try
            {
                var fullPath = Path.Combine(Constants.DATA_BASEPATH, filename);
                if (!System.IO.File.Exists(fullPath))
                {
                    return; //file was deleted... maybe this file was a temporary file that was cleaned up?
                }
                text = System.IO.File.ReadAllText(fullPath);
                break;
            }
            catch (IOException)
            {
                if (i == 4)
                {
                    throw;
                }
            }
            await Task.Delay(50 + 150 * i);
            //once again, we need to debounce concurrent operations on the same file
            _fileChangeTokenDict.TryGetValue(filename, out currentToken);
            if (token != currentToken)
            {
                return;
            }
        }
        var tabContent = new TabContent
        {
            FileId = fileId,
            Text = text!
        };
        connectionIds = _tabSubscriptionService.GetConnectionIds(fileId);
        await _hubContext.Clients.Clients(connectionIds).SendAsync("tabContent", tabContent);
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
