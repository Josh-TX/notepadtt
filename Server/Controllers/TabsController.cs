using Microsoft.AspNetCore.Mvc;

[Route("api/tabs")]
public class TabsController : Controller
{
    private readonly TabContentService _tabContentService;

    public TabsController(TabContentService tabContentService)
    {
        _tabContentService = tabContentService;
    }

    [HttpGet]
    [Route("{fileId}")]
    public TabContent GetTabContent([FromRoute] Guid fileId)
    {
        var tabContent = _tabContentService.LoadTabContent(fileId);
        return tabContent;
    }
}