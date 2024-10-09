import { Injectable, WritableSignal, signal } from '@angular/core';

@Injectable({
    providedIn: 'root',
})
export class FooterService {
 
    private _undoHandler: (() => any) | null = null;
    private _redoHandler: (() => any) | null = null;
    $wordWrap: WritableSignal<boolean>;
    $canUndo: WritableSignal<boolean>;
    $canRedo: WritableSignal<boolean>;
    constructor(
    ){
        var wordWrap = localStorage["word-wrap"] === "true"
        this.$wordWrap = signal(wordWrap);
        this.$canUndo = signal(false);
        this.$canRedo = signal(false);
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
        this.$wordWrap.set(!this.$wordWrap());
    }
}