import { Component, ViewChild, Inject, ElementRef, Signal, computed, effect } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterOutlet } from '@angular/router';
import { IRealTimeService, TabContent, TabInfo } from '../services/real-time.service';
import { animate, style, transition, trigger } from '@angular/animations';
import { IFileLoaderService } from '../services/file-loader.service';


@Component({
  selector: 'tabset',
  standalone: true,
  imports: [CommonModule, RouterOutlet],
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
    deletedTabInfo: TabInfo | undefined;
    deletedTabIndex: number | undefined;
    deletedTabContent: TabContent | undefined;
    deletedTabTimeout: any;
    $tabInfos: Signal<TabInfo[]>;
    $errorState: Signal<boolean>;



    constructor(
        private realTimeService: IRealTimeService,
        private fileLoaderService: IFileLoaderService
    ){
        this.$errorState = computed(() => this.realTimeService.$errorMessage()!= null);
        this.$tabInfos = computed(() => {
            var tabInfos = this.realTimeService.$info()!.tabInfos;
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
                    this.realTimeService.subscribeTabContent(this.activeFileId);
                })
            }
            return tabInfos;
        });
        this.activeFileId = this.realTimeService.$info()!.activeFileId;
        this.activeIndex = this.$tabInfos().findIndex(z => z.fileId == this.activeFileId);
        this.removeContextMenu = this.removeContextMenu.bind(this);
        if (this.activeFileId){
            this.realTimeService.subscribeTabContent(this.activeFileId);
        }
    }

    tabClicked(fileId: string){
        if (fileId != this.activeFileId){
            this.activeFileId = fileId;
            this.realTimeService.subscribeTabContent(this.activeFileId);
            this.activeIndex = this.$tabInfos().findIndex(z => z.fileId == fileId);
            this.realTimeService.setInfo({
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
        this.contextMenuFileId = undefined;
    }

    delete(fileId: string){
        var tabInfos = this.$tabInfos();
        var deletedTabInfo = tabInfos.find(z => z.fileId == fileId);
        if (deletedTabInfo!.isProtected){
            if (!confirm(`${deletedTabInfo!.filename} is protected. Delete anyways?`)){
                return;
            }
        }
        this.deletedTabIndex = tabInfos.findIndex(z => z.fileId == fileId);
        this.contextMenuFileId = undefined;
        this.fileLoaderService.load(fileId).then((tabContent: TabContent) => {
            this.deletedTabInfo = deletedTabInfo;
            this.deletedTabContent = tabContent;
            clearTimeout(this.deletedTabTimeout);
            this.deletedTabTimeout = setTimeout(() => {
                this.deletedTabInfo = undefined;
                this.deletedTabContent = undefined;
            }, 3500);
            tabInfos = tabInfos.filter(z => z.fileId != fileId);
            this.activeIndex = Math.min(this.activeIndex, tabInfos.length - 1);
            this.activeFileId = this.activeIndex >= 0
                ? tabInfos[this.activeIndex].fileId
                : null;
            this.realTimeService.subscribeTabContent(this.activeFileId);
            this.realTimeService.setInfo({
                activeFileId: this.activeFileId,
                tabInfos: tabInfos
            });
        })
    }

    undoDelete(){
        var tabInfos = this.$tabInfos();
        if (tabInfos.some(z => z.filename == this.deletedTabInfo!.filename)){
            var altFileName = "new " + this.getNextNewNum();
            alert(`a file named "${this.deletedTabInfo!.filename}" already exists. Recovered file will instead be called "${altFileName}"`);
            this.deletedTabInfo!.filename = altFileName;
        }
        var index = Math.min(tabInfos.length, this.deletedTabIndex!);
        tabInfos.splice(index, 0, this.deletedTabInfo!);
        this.activeIndex = index;
        this.activeFileId = tabInfos[this.activeIndex].fileId;
        this.realTimeService.subscribeTabContent(this.activeFileId);
        this.realTimeService.setInfo({
            activeFileId: this.activeFileId,
            tabInfos: tabInfos
        });
        var recoveredTabContent = this.deletedTabContent!;
        //we need to give the setInfo time to save on the server
        //otherwise tabContent change won't work because the fileId isn't found
        //I know... this isn't the best way to handle race conditions
        setTimeout(() => {
            this.realTimeService.tabContentChanged(recoveredTabContent, true);
        }, 100);
        clearTimeout(this.deletedTabTimeout);
        this.deletedTabInfo = undefined;
        this.deletedTabContent = undefined;
    }

    rename(){
        var tabInfos = this.$tabInfos();
        var tabInfo = tabInfos.find(z => z.fileId == this.contextMenuFileId)!;
        while (true){
            var newName = prompt("enter a new name", tabInfo.filename);
            if (!newName){
                break;
            }
            if (/[\/\0]/.test(newName)){
                alert(`the provided name contains invalid characters`);
                continue;
            }
            if (tabInfos.some(z => z != tabInfo && z.filename == newName) || newName == ".notepadtt_tab_data.txt"){
                alert(`a file named "${newName}" already exists`);
                continue;
            }
            tabInfo.filename = newName;
            this.realTimeService.setInfo({
                activeFileId: this.activeFileId,
                tabInfos: tabInfos
            });
            break;
        }
    }

    isProtected(fileId: string): boolean{
        return this.$tabInfos().find(z => z.fileId == fileId)!.isProtected;
    }

    toggleProtection(){
        var tabInfos = this.$tabInfos();
        var tabInfo = tabInfos.find(z => z.fileId == this.contextMenuFileId)!;
        tabInfo.isProtected = !tabInfo.isProtected;
        this.realTimeService.setInfo({
            activeFileId: this.activeFileId,
            tabInfos: tabInfos
        });
    }


    moveToLeft(){
        var tabInfos = this.$tabInfos();
        var tabInfo = tabInfos.find(z => z.fileId == this.contextMenuFileId)!;
        tabInfos = [tabInfo, ...tabInfos.filter(z => z != tabInfo)];
        this.realTimeService.setInfo({
            activeFileId: this.activeFileId,
            tabInfos: tabInfos
        });
    }

    duplicate(){
        var originalFileId = this.contextMenuFileId!
        var newFileId = this.randomGuid();
        var tabInfos = this.$tabInfos();
        var originalTabInfo = tabInfos.find(z => z.fileId == originalFileId)!;
        var originalTabIndex = tabInfos.findIndex(z => z.fileId == originalFileId);
        this.fileLoaderService.load(originalFileId).then((originalTabContent: TabContent) => {
            var newTabInfo = {
                filename: this.getDuplicateFilename(originalTabInfo.filename),
                fileId: newFileId,
                isProtected: originalTabInfo.isProtected
            }
            var newTabIndex = Math.min(tabInfos.length, originalTabIndex + 1);
            tabInfos.splice(newTabIndex, 0, newTabInfo);
            this.activeIndex = newTabIndex;
            this.activeFileId = tabInfos[this.activeIndex].fileId;
            this.realTimeService.subscribeTabContent(this.activeFileId);
            this.realTimeService.setInfo({
                activeFileId: this.activeFileId,
                tabInfos: tabInfos
            });
            var newTabContent: TabContent = {
                fileId: newFileId,
                text: originalTabContent.text
            }
            //we need to give the setInfo time to save on the server
            //otherwise tabContent change won't work because the fileId isn't found
            //I know... this isn't the best way to handle race conditions
            setTimeout(() => {
                this.realTimeService.tabContentChanged(newTabContent, true);
            }, 100);
        })
    }

    download(){
        var filename = this.$tabInfos().find(z => z.fileId == this.contextMenuFileId)!.filename;
        this.fileLoaderService.load(this.contextMenuFileId!).then((tabContent: TabContent) => {
            const blob = new Blob([tabContent.text], { type: 'text/plain' });
            const link = document.createElement('a');
            link.download = filename;
            link.href = window.URL.createObjectURL(blob);
            document.body.appendChild(link);
            link.click();
            document.body.removeChild(link);
            window.URL.revokeObjectURL(link.href);
        })
    }

    newTab(){
        var index = this.getNextNewNum();
        var newFileId = this.randomGuid();
        var tabInfos = this.$tabInfos();
        tabInfos.push({
            filename: "new " + index,
            fileId: newFileId,
            isProtected: false
        });
        this.activeFileId = newFileId;
        this.realTimeService.subscribeTabContent(this.activeFileId);
        this.activeIndex = tabInfos.length - 1;
        this.realTimeService.setInfo({
            activeFileId: newFileId,
            tabInfos: tabInfos
        });
    }

    private getDuplicateFilename(name: string): string {
        var tabInfos = this.$tabInfos();
        var anyDuplicateNames = tabInfos.some(z => z.filename == name);
        if (!anyDuplicateNames){
            return name; //shouldn't happen
        }
        var matches = name.match(/^(.*?)(?: *\(\d+\))$/)
        var baseName = matches ? matches[1] : name;
        for (var i = 2; i < 10000; i++){
            var candidate = `${baseName} (${i})`;
            if (!tabInfos.some(z => z.filename == candidate)){
                return candidate;
            }
        }
        alert("you have too many files");
        throw "exceeded file limit";
    }

    private getNextNewNum(): number {
        var tabInfos = this.$tabInfos();
        for (var i = 1; i < 10000; i++){
            if (!tabInfos.some(z => z.filename == "new " + i)){
                return i;
            }
        }
        alert("you have too many files");
        throw "exceeded file limit";
    }

    private randomGuid(): string {
        //copied from stackOverflow
        return "10000000-1000-4000-8000-100000000000".replace(/[018]/g, (c: any) =>
            (c ^ crypto.getRandomValues(new Uint8Array(1))[0] & 15 >> c / 4).toString(16)
        );
    }
}