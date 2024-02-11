import { HttpClient } from '@angular/common/http';
import { Injectable, NgZone, Signal, WritableSignal, signal } from '@angular/core';
import * as signalR from '@microsoft/signalr';
//namespace signalR { export type HubConnection = any;}

export type TabInfo = {
    filename: string,
    fileId: string
}

export type TabContent = {
    fileId: string
    text: string,
}

export type Info = {
    activeFileId: string | null,
    tabInfos: TabInfo[],
}

@Injectable({
    providedIn: 'root',
})
export class SignalRService {

    $info: WritableSignal<Info | null>;

    $tabContent: WritableSignal<TabContent | null>;
    private fileId: string | null = null;

    private connection: signalR.HubConnection;
    private infoPromiseDebouncer: PromiseDebouncer;

    constructor(
        private zone: NgZone
    ){
        this.$info = signal(null);
        this.$tabContent = signal(null);
        var signalR = (<any>window).signalR
        this.connection = new signalR.HubConnectionBuilder()
            .withUrl('main-hub')
            .build();
        this.connection.on("info", (info: Info) => {
            this.zone.run(() => {
                this.$info.set(info);
            })
        });
        this.connection.onclose(() => {
            this.connection
        })
        this.connection.on("tabContent", (tabContent: TabContent) => {
            if (this.$tabContent && tabContent.fileId == this.fileId){
                this.$tabContent.set(tabContent);
            }
        });
        var startPromise = this.connection.start();
        this.infoPromiseDebouncer = new PromiseDebouncer(startPromise);
    }

    setInfo(info: Info){
        this.$info.set(info);
        this.infoPromiseDebouncer.queue(() => {
            this.connection.invoke("InfoChanged", info);
        });
    }

    subscribeTabContent(fileId: string | null){
        if (this.fileId == fileId){
            return;
        }
        if (this.fileId){
            this.connection.invoke("UnsubscribeTabContent", this.fileId);
        }
        if (fileId){
            this.connection.invoke("SubscribeTabContent", fileId);
            this.$tabContent.set(null);
        }
        this.fileId = fileId;
    }

    tabContentChanged(tabContent: TabContent){
        this.connection.invoke("TabContentChanged", tabContent);
    }
}


class PromiseDebouncer {

    private activePromise: Promise<any>;
    private nextFunc: (() => any) | undefined;

    constructor(promise: Promise<any>){
        this.activePromise = promise;
    }

    queue(func: () => any){
        this.nextFunc = func;
        this.activePromise = this.activePromise.then(() => {
            if (this.nextFunc){
                this.nextFunc();
                this.nextFunc = undefined;
                //not sure if infinite-chaining thens can cause a stack overflow, so just in case:
                this.activePromise = Promise.resolve();
            }
        });
    }

}