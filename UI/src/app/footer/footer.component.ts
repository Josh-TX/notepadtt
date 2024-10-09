import { Component, Signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FooterService } from '../services/footer.service';


@Component({
  selector: 'footer',
  standalone: true,
  imports: [CommonModule],
  templateUrl: './footer.component.html',
  styleUrls: ['./footer.component.scss']
})
export class FooterComponent {

    $wordWrap: Signal<boolean>
    $canUndo: Signal<boolean>
    $canRedo: Signal<boolean>

    constructor(
        private footerService: FooterService
    ){
        this.$wordWrap = footerService.$wordWrap;
        this.$canUndo = footerService.$canUndo;
        this.$canRedo = footerService.$canRedo;
    }

    undo(){
        this.footerService.undo();
    }

    redo(){
        this.footerService.redo();
    }

    toggleWordWrap(){
        this.footerService.toggleWordWrap();
    }
}