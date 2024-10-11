import { HttpClient } from '@angular/common/http';
import { Injectable, NgZone, Signal, WritableSignal, signal } from '@angular/core';
import * as signalR from '@microsoft/signalr';
//namespace signalR { export type HubConnection = any;}

@Injectable({
    providedIn: 'root',
})
export abstract class IRealTimeService {
    abstract $info: WritableSignal<Info | null>;
    abstract $tabContent: WritableSignal<TabContent | null>;
    abstract $errorMessage: WritableSignal<{message: string} | null>;

    
    abstract setInfo(info: Info): void
    abstract subscribeTabContent(fileId: string | null): void
    abstract tabContentChanged(tabContent: TabContent, updateSignal?: boolean): void
}

export type TabInfo = {
    filename: string,
    fileId: string,
    isProtected: boolean
}

export type TabContent = {
    fileId: string
    text: string,
}

export type Info = {
    activeFileId: string | null,
    tabInfos: TabInfo[]
}

export type FullInfo = {
    activeFileId: string | null,
    tabInfos: TabInfo[],
    changeToken: string
}

@Injectable({
    providedIn: 'root',
})
export class SignalRRealTimeService implements IRealTimeService {

    $info: WritableSignal<Info | null>;
    $tabContent: WritableSignal<TabContent | null>;
    $errorMessage: WritableSignal<{message: string} | null>;

    private fileId: string | null = null;
    private connection: signalR.HubConnection;
    private actualSub: string | null = null;
    private desiredSub: string | null = null;
    private infoToUpdate: Info | null = null;
    private isConnected: boolean = false;
    private changeToken: string | null = null;

    constructor(
        //signalR messages are received outside of the NgZone
        private zone: NgZone
    ){
        console.log("SignalRRealTimeService");
        this.$info = signal(null);
        this.$errorMessage = signal(null);
        this.$tabContent = signal(null);
        var signalR = (<any>window).signalR
        this.connection = new signalR.HubConnectionBuilder()
            .withUrl('main-hub')
            //.configureLogging(signalR.LogLevel.Debug)
            .withAutomaticReconnect()
            .build();
        this.connection.on("info", (info: FullInfo) => {
            this.changeToken = info.changeToken;
            this.zone.run(() => {
                this.$info.set(info);
            })
        });
        this.connection.onreconnecting(error => {
            this.$errorMessage.set({message: "disconnected with server. Reconnecting..."});
            this.actualSub = null;
            this.isConnected = false;
        });
        this.connection.onreconnected(() => {
            this.onConnected();
        });
        this.connection.onclose((error) => {
            this.$errorMessage.set({message: "lost connection with server"});
        })
        this.connection.on("tabContent", (tabContent: TabContent) => {
            if (this.$tabContent && tabContent.fileId == this.fileId){
                this.$tabContent.set(tabContent);
            }
        });
        this.connection.start().then(() => this.onConnected())
    }

    private onConnected(){
        this.zone.run(() => {
            this.isConnected = true;
            this.$errorMessage.set(null);
            this.tryUpdateSubscriptions();
            if (this.infoToUpdate != null){
                this.connection.invoke("InfoChanged", this.infoToUpdate).catch((e) => this.$errorMessage.set({message: "error saving data on server"}));
                this.infoToUpdate = null;
            }
        })
    }

    setInfo(info: Info){
        this.$info.set(info);
        var fullinfo: FullInfo = {
            ...info,
            changeToken: this.changeToken!
        }
        if (this.isConnected){
            this.connection.invoke("InfoChanged", fullinfo).catch((e) => this.$errorMessage.set({message: "error saving data on server"}));
            this.infoToUpdate = null;
        } else {
            this.infoToUpdate = info;
        }
    }

    subscribeTabContent(fileId: string | null){
        if (this.fileId == fileId){
            return;
        }
        this.$tabContent.set(null);
        this.fileId = fileId;
        this.desiredSub = fileId;
        this.tryUpdateSubscriptions();
    }

    tabContentChanged(tabContent: TabContent, updateSignal: boolean = false){
        this.connection.invoke("TabContentChanged", tabContent).catch((e) => this.$errorMessage.set({message: "error saving file"}));
        if (updateSignal){
            this.$tabContent.set(tabContent);
        }
    }


    private tryUpdateSubscriptions(){
        if (!this.isConnected || this.actualSub == this.desiredSub){
            return;
        }
        if (this.actualSub){
            this.connection.invoke("UnsubscribeTabContent", this.actualSub).catch((e) => this.$errorMessage.set({message: "error synchronizing with server"}));
        }
        if (this.desiredSub){
            this.connection.invoke("SubscribeTabContent", this.desiredSub).catch((e) => this.$errorMessage.set({message: "error synchronizing with server"}));
        }
        this.actualSub = this.desiredSub;
    }
}



export class MockRealTimeService implements IRealTimeService {
    oldInfo: Info;
    $info: WritableSignal<Info>;
    $tabContent: WritableSignal<TabContent | null>;
    $errorMessage: WritableSignal<{ message: string; } | null>;

    constructor(){
        console.log("MockRealTimeService");
        var info: Info = localStorage["notepadtt_info_json"] ? JSON.parse(localStorage["notepadtt_info_json"]) : this.getDefaultInfo();
        this.$info = signal(info);
        this.oldInfo = JSON.parse(JSON.stringify(info));
        var activeTabInfo = info.tabInfos.find(z => z.fileId == info.activeFileId);
        var tabContent: TabContent | null = null;
        if (activeTabInfo){
            var tabContentStr = localStorage["notepadtt:" + activeTabInfo.filename];
            if (tabContentStr != null){
                tabContent = {
                    fileId: activeTabInfo.fileId,
                    text: tabContentStr
                }
            }
        }
        this.$errorMessage = signal(null);
        this.$tabContent = signal(tabContent);
    }

    setInfo(info: Info): void {
        for (var oldTabInfo of this.oldInfo.tabInfos){
            var foundNewInfo =  info.tabInfos.find(z => z.fileId == oldTabInfo.fileId);
            if (foundNewInfo == null){
                delete localStorage["notepadtt:" + oldTabInfo.filename];
            } else {
                if (foundNewInfo.filename != oldTabInfo.filename){
                    localStorage["notepadtt:" + foundNewInfo.filename] = localStorage["notepadtt:" + oldTabInfo.filename];
                    delete localStorage["notepadtt:" + oldTabInfo.filename];
                }
            }
        }
        this.$info.set(info);
        this.oldInfo = JSON.parse(JSON.stringify(info));
        localStorage["notepadtt_info_json"] = JSON.stringify(info);
    }

    subscribeTabContent(fileId: string | null): void {
        var tabInfo = this.$info().tabInfos.find(z => z.fileId == fileId);
        if (tabInfo){
            var tabContentStr = localStorage["notepadtt:" + tabInfo.filename] || "\n\n\n\n";
            this.$tabContent.set({
                fileId: tabInfo.fileId,
                text: tabContentStr
            });
        } else {
            this.$tabContent.set(null);
        }

    }
    
    tabContentChanged(tabContent: TabContent, updateSignal?: boolean): void {
        var tabInfo = this.$info().tabInfos.find(z => z.fileId == tabContent.fileId);
        if (tabInfo){
            localStorage["notepadtt:" + tabInfo.filename] = tabContent.text;
        }
        if (updateSignal){
            this.$tabContent.set(tabContent);
        }
    }

    private getDefaultInfo(): Info{
        var fileId = this.randomGuid();
        var info: Info = { 
            activeFileId: fileId,
            tabInfos: [
                {
                    fileId: fileId,
                    filename: "new 1",
                    isProtected: false,
                }
            ]
        }
        localStorage["notepadtt_info_json"] = JSON.stringify(info);
        localStorage["notepadtt:new 1"] = "This is a demo of notepadtt, but hosted on github pages instead of self-hosted via docker.\n\nThis demo site will write to localStorage, whereas normally the app would real-time sync with the server\n\nmore info at https://github.com/Josh-TX/notepadtt"
        return info;
    }
    
    private randomGuid(): string {
        //copied from stackOverflow
        return "10000000-1000-4000-8000-100000000000".replace(/[018]/g, (c: any) =>
            (c ^ crypto.getRandomValues(new Uint8Array(1))[0] & 15 >> c / 4).toString(16)
        );
    }
}