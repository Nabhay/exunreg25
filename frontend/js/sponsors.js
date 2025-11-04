document.addEventListener("DOMContentLoaded", function(){
  var imgs = document.querySelectorAll('.sponsor-img');
  for(var i=0;i<imgs.length;i++){
    var img = imgs[i];
    var h = img.getAttribute('data-height');
    if(!h) h = '80px';
    img.style.height = h;
    img.style.width = 'auto';
  }
});
