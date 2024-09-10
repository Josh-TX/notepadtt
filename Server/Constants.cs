

public class Constants
{
    //local debugging is easier (on Windows at least) if it's a relative path. 
#if DEBUG
    public const string DATA_BASEPATH = "data";
#else
    public const string DATA_BASEPATH = "/data";
#endif

    public const string METADATA_FILENAME = ".notepadtt_metadata.txt";

}
