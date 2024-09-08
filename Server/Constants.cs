

public class Constants
{
    //local debugging is easier (on Windows at least) if it's a relative path. 
#if DEBUG
    public const string DATA_BASEPATH = "data";
#else
    public const string DATA_BASEPATH = "/data";
#endif

    public const string TAB_DATA_FILENAME = ".notepadtt_tab_data.txt";

}
