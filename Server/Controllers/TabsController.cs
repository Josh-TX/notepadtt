using Microsoft.AspNetCore.Mvc;

[Route("api/tabs")]
public class TabsController : Controller
{

    [HttpGet]
    [Route("{fileId}")]
    public TabContent GetTabContent([FromRoute] Guid fileId)
    {
        var storageManager = new StorageManager();
        var tabContent = storageManager.LoadTabContent(fileId);
        return tabContent;
    }
}