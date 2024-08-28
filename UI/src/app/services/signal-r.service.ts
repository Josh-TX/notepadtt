import { HttpClient } from '@angular/common/http';
import { Injectable, NgZone, Signal, WritableSignal, signal } from '@angular/core';
import * as signalR from '@microsoft/signalr';
//namespace signalR { export type HubConnection = any;}

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

    private actualSub: string | null = null;
    private desiredSub: string | null = null;
    private infoToUpdate: Info | null = null;
    private isConnected: boolean = false;

    constructor(
        private zone: NgZone
    ){
        this.$info = signal(null);
        this.$tabContent = signal(null);
        var signalR = (<any>window).signalR
        this.connection = new signalR.HubConnectionBuilder()
            .withUrl('main-hub')
            .configureLogging(signalR.LogLevel.Debug)
            .withAutomaticReconnect()
            .build();
        this.connection.on("info", (info: Info) => {
            this.zone.run(() => {
                this.$info.set(info);
            })
        });
        this.connection.onreconnecting(error => {
            console.log(`Connection lost due to error "${error}". Reconnecting.`);
            document.body.setAttribute("style", "--primary: #d22d2d");
            this.actualSub = null;
            this.isConnected = false;
        });
        this.connection.onreconnected(() => {
            console.log(`Connection reestablished`);
            document.body.setAttribute("style", "--primary: #0078D4")
            this.onConnected();
        });
        this.connection.on("tabContent", (tabContent: TabContent) => {
            if (this.$tabContent && tabContent.fileId == this.fileId){
                this.$tabContent.set(tabContent);
            }
        });
        this.connection.start().then(() => this.onConnected())
    }

    private onConnected(){
        this.isConnected = true;
        this.tryUpdateSubscriptions();
        if (this.infoToUpdate != null){
            this.connection.invoke("InfoChanged", this.infoToUpdate);
            this.infoToUpdate = null;
        }
    }

    setInfo(info: Info){
        this.$info.set(info);
        if (this.isConnected){
            this.connection.invoke("InfoChanged", info);
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
        this.connection.invoke("TabContentChanged", tabContent);
        if (updateSignal){
            this.$tabContent.set(tabContent);
        }
    }

    private tryUpdateSubscriptions(){
        if (!this.isConnected || this.actualSub == this.desiredSub){
            return;
        }
        if (this.actualSub){
            this.connection.invoke("UnsubscribeTabContent", this.actualSub);
        }
        if (this.desiredSub){
            this.connection.invoke("SubscribeTabContent", this.desiredSub);
        }
        this.actualSub = this.desiredSub;
    }
}