import { Component, ViewChild, Inject, ElementRef, Signal, computed, effect } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterOutlet } from '@angular/router';
import { SignalRService, TabContent, TabInfo } from '../services/signal-r.service';
import { BrowserAnimationsModule } from '@angular/platform-browser/animations';
import { animate, style, transition, trigger } from '@angular/animations';


@Component({
  selector: 'tabset',
  standalone: true,
  imports: [CommonModule, RouterOutlet, BrowserAnimationsModule],
  templateUrl: './tabset.component.html',
  styleUrls: ['./tabset.component.scss'],
  animations: [
    trigger(
      'inOutAnimation', 
      [
        transition(
          ':enter', 
          [
            style({ bottom: 0, opacity: 0 }),
            animate('0.25s ease-out', 
                    style({ bottom: "3vh", opacity: 1 }))
          ]
        ),
        transition(
          ':leave', 
          [
            style({  bottom: "3vh", opacity: 1 }),
            animate('0.25s ease-in', 
                    style({ bottom: 0, opacity: 0 }))
          ]
        )
      ]
    )
  ]
})
export class TabsetComponent {
    @ViewChild("contextMenu") contextMenu: ElementRef | undefined;
    activeFileId: string | null;
    activeIndex: number;
    contextMenuFileId: string | undefined;
    closedTabInfo: TabInfo | undefined;
    closedTabIndex: number | undefined;
    closedTabContent: TabContent | undefined;
    closedTabTimeout: any;
    $tabInfos: Signal<TabInfo[]>
    constructor(
        private signalRService: SignalRService
    ){
        this.$tabInfos = computed(() => {
            var tabInfos = this.signalRService.$info()!.tabInfos;
            if (this.activeFileId){
                if (!tabInfos.some(z => z.fileId == this.activeFileId)){
                    this.activeIndex = Math.min(this.activeIndex, tabInfos.length - 1);
                    this.activeFileId = this.activeIndex >= 0
                        ? tabInfos[this.activeIndex].fileId
                        : null;
                }
            } else if (tabInfos.length) {
                this.activeFileId = tabInfos[0].fileId;
                //angular doesn't like triggering signals within computed
                setTimeout(() => {
                    this.signalRService.subscribeTabContent(this.activeFileId);
                })
            }
            return tabInfos;
        });
        this.activeFileId = this.signalRService.$info()!.activeFileId;
        this.activeIndex = this.$tabInfos().findIndex(z => z.fileId == this.activeFileId);
        this.removeContextMenu = this.removeContextMenu.bind(this);
        if (this.activeFileId){
            this.signalRService.subscribeTabContent(this.activeFileId);
        }
    }

    tabClicked(fileId: string){
        if (fileId != this.activeFileId){
            this.activeFileId = fileId;
            this.signalRService.subscribeTabContent(this.activeFileId);
            this.activeIndex = this.$tabInfos().findIndex(z => z.fileId == fileId);
            this.signalRService.setInfo({
                activeFileId: this.activeFileId,
                tabInfos: this.$tabInfos()
            });
        }
    }

    onRightClick(event: MouseEvent, fileId: string){
        event.preventDefault();
        var menu: HTMLElement = this.contextMenu!.nativeElement;
        menu.classList.add("active");
        menu.setAttribute("style", `left: ${event.clientX}px; top: ${event.clientY}px`);
        this.contextMenuFileId = fileId;
        document.addEventListener("click", this.removeContextMenu)
    }

    removeContextMenu(){
        document.removeEventListener("click", this.removeContextMenu);
        var menu: HTMLElement = this.contextMenu!.nativeElement;
        menu.classList.remove("active");
    }

    close(fileId: string){
        var tabInfos = this.$tabInfos();
        var closedTabInfo = tabInfos.find(z => z.fileId == fileId);
        this.closedTabIndex = tabInfos.findIndex(z => z.fileId == fileId);
        fetch("/api/tabs/" + fileId).then(response => response.json()).then((tabContent: TabContent) => {
            this.closedTabInfo = closedTabInfo;
            this.closedTabContent = tabContent;
            clearTimeout(this.closedTabTimeout);
            this.closedTabTimeout = setTimeout(() => {
                this.closedTabInfo = undefined;
                this.closedTabContent = undefined;
            }, 3500);
            tabInfos = tabInfos.filter(z => z.fileId != fileId);
            this.activeIndex = Math.min(this.activeIndex, tabInfos.length - 1);
            this.activeFileId = this.activeIndex >= 0
                ? tabInfos[this.activeIndex].fileId
                : null;
            this.signalRService.subscribeTabContent(this.activeFileId);
            this.signalRService.setInfo({
                activeFileId: this.activeFileId,
                tabInfos: tabInfos
            });
        })
    }

    undo(){
        var tabInfos = this.$tabInfos();
        if (tabInfos.some(z => z.filename == this.closedTabInfo!.filename)){
            alert(`a file named "${this.closedTabInfo!.filename}" already exists`);
            return;
        }
        var index = Math.min(tabInfos.length, this.closedTabIndex!);
        tabInfos.splice(index, 0, this.closedTabInfo!);
        this.activeIndex = index;
        this.activeFileId = tabInfos[this.activeIndex].fileId;
        this.signalRService.subscribeTabContent(this.activeFileId);
        this.signalRService.setInfo({
            activeFileId: this.activeFileId,
            tabInfos: tabInfos
        });
        var recoveredTabContent = this.closedTabContent!;
        //we need to give the setInfo time to save on the server
        //otherwise tabContent change won't work because the fileId isn't found
        //I know... this isn't the best way to handle race conditions
        setTimeout(() => {
            this.signalRService.tabContentChanged(recoveredTabContent, true);
        }, 100);
        clearTimeout(this.closedTabTimeout);
        this.closedTabInfo = undefined;
        this.closedTabContent = undefined;
    }

    rename(){
        var newName = prompt("enter a name");
        if (newName){
            var tabInfos = this.$tabInfos();
            var tabInfo = tabInfos.find(z => z.fileId == this.contextMenuFileId)!;
            if (tabInfos.some(z => z != tabInfo && z.filename == newName)){
                alert(`a file named "${newName}" already exists`);
                return;
            }
            tabInfo.filename = newName;
            this.signalRService.setInfo({
                activeFileId: this.activeFileId,
                tabInfos: tabInfos
            });
        }
    }

    newTab(){
        var tabInfos = this.$tabInfos();
        for (var i = 1; i < 1000; i++){
            if (!tabInfos.some(z => z.filename == "new " + i)){
                var fileId = this.randomGuid();
                tabInfos.push({
                    filename: "new " + i,
                    fileId: fileId,
                });
                this.activeFileId = fileId;
                this.signalRService.subscribeTabContent(this.activeFileId);
                this.activeIndex = tabInfos.length - 1;
                this.signalRService.setInfo({
                    activeFileId: fileId,
                    tabInfos: tabInfos
                });
                return;
            }
        }
        alert("you have too many files");
    }

    private randomGuid(): string {
        //copied from stackOverflow
        return "10000000-1000-4000-8000-100000000000".replace(/[018]/g, (c: any) =>
            (c ^ crypto.getRandomValues(new Uint8Array(1))[0] & 15 >> c / 4).toString(16)
        );
    }
}


function computedUntilNotNull(func: () => any){
    var val: any = null;
    computed(() => {
        if (val != null){
            return val;
        }
        var res = func();
        if (res != null){
            val = res;
        }
        return res;
    })
}