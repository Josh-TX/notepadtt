import { Injectable, WritableSignal, signal } from '@angular/core';

export type FooterData = {
    length?: number | undefined,
    lines?: number | undefined,
    ln?: number | undefined,
    col?: number | undefined,
    pos?: number | undefined,
    selectedLength?: number | undefined,
    selectedLines?: number | undefined,
}

@Injectable({
    providedIn: 'root',
})
//this service merely facilitates communication between the tabset component and the footer component
export class FooterService {
 
    private _undoHandler: (() => any) | null = null;
    private _redoHandler: (() => any) | null = null;
    $wordWrap: WritableSignal<boolean>;
    $canUndo: WritableSignal<boolean>;
    $canRedo: WritableSignal<boolean>;
    $footerData: WritableSignal<FooterData>;
    constructor(
    ){
        var wordWrap = localStorage["word-wrap"] === "true"
        this.$wordWrap = signal(wordWrap);
        this.$canUndo = signal(false);
        this.$canRedo = signal(false);
        this.$footerData = signal({});
    }

    registerUndoHandler(handler: () => any){
        this._undoHandler = handler;
    }

    registerRedoHandler(handler: () => any){
        this._redoHandler = handler;
    }

    undo(){
        if (this._undoHandler){
            this._undoHandler();
        }
    }

    redo(){
        if (this._redoHandler){
            this._redoHandler();
        }
    }

    toggleWordWrap(){
        var newWordWrap = !this.$wordWrap();
        localStorage["word-wrap"] = newWordWrap.toString();
        this.$wordWrap.set(newWordWrap);
    }
}