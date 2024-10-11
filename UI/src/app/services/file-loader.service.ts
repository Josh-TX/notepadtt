import { Injectable, WritableSignal, signal } from '@angular/core';
import { IRealTimeService, TabContent } from './real-time.service';

@Injectable({
    providedIn: 'root',
})
export abstract class IFileLoaderService {
    abstract load(fileId: string): Promise<TabContent>
}

@Injectable({
    providedIn: 'root',
})
//this service merely facilitates communication between the tabset component and the footer component
export class ApiFileLoaderService implements IFileLoaderService {
    load(fileId: string): Promise<TabContent>{
        return fetch("/api/tabs/" + fileId).then(response => response.json())
    }    
}

@Injectable({
    providedIn: 'root',
})
//this service merely facilitates communication between the tabset component and the footer component
export class MockFileLoaderService implements IFileLoaderService {
    constructor(private realTimeService: IRealTimeService){
       
    }

    load(fileId: string): Promise<TabContent>{
        var tabInfo = this.realTimeService.$info()?.tabInfos.find(z => z.fileId == fileId);
        var text = "\n\n\n\n"
        if (tabInfo){
            text = localStorage["notepadtt:" + tabInfo.filename] || "\n\n\n\n";
        }
        return Promise.resolve({
            fileId: fileId,
            text: text
        })
    }    
}