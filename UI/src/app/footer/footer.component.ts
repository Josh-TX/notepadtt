import { Component, computed, effect, Signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FooterData, FooterService } from '../services/footer.service';
import { FormsModule } from '@angular/forms';


@Component({
    selector: 'footer',
    standalone: true,
    imports: [CommonModule, FormsModule],
    templateUrl: './footer.component.html',
    styleUrls: ['./footer.component.scss']
})
export class FooterComponent {

    $wordWrap: Signal<boolean>;
    $canUndo: Signal<boolean>;
    $canRedo: Signal<boolean>;
    $footerData: Signal<FooterData>;

    $isFindActive: Signal<boolean>;
    $findMatchCount: Signal<number>;
    $findPos: Signal<number>;
    $findNextDisabled: Signal<boolean>;
    $findPrevDisabled: Signal<boolean>;
    searchTextBinding: string = "";

    constructor(
        private footerService: FooterService
    ) {
        this.$wordWrap = footerService.$wordWrap;
        this.$canUndo = footerService.$canUndo;
        this.$canRedo = footerService.$canRedo;
        this.$footerData = footerService.$footerData;

        this.$isFindActive = footerService.$isFindActive;
        this.$findMatchCount = footerService.$findMatchCount;
        this.$findPos = computed(() => footerService.$findMatchCount() > 0 ? footerService.$findIndex() + 1 : 0);
        this.$findNextDisabled = computed(() => footerService.$findIndex() >= footerService.$findMatchCount() - 1);
        this.$findPrevDisabled = computed(() => footerService.$findIndex() <= 0);
    }

    undo() {
        this.footerService.undo();
    }

    redo() {
        this.footerService.redo();
    }

    toggleFindActive() {
        this.footerService.updateFindActive(!this.$isFindActive());
    }

    toggleWordWrap() {
        this.footerService.toggleWordWrap();
    }

    updateFindText(str: string) {
        this.footerService.updateFindText(str);
    }

    stopFind() {
        this.footerService.updateFindActive(false);
    }

    findNext() {
        if (this.$findMatchCount() > 1) {
            this.footerService.updateFindIndex((this.footerService.$findIndex() + 1 + this.$findMatchCount()) % this.$findMatchCount())
        }
    }
    findPrev() {
        if (this.$findMatchCount() > 1) {
            this.footerService.updateFindIndex((this.footerService.$findIndex() - 1 + this.$findMatchCount()) % this.$findMatchCount())
        }
    }

    findKeyDown(event: KeyboardEvent) {
        if (event.key === 'Enter') {
            if (event.shiftKey) {
                this.findPrev();
            } else {
                this.findNext();
            }
        }
    }
}